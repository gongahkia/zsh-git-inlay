// Package gitstate builds an exact identity for candidate-producing state.
package gitstate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
)

const GeneratorVersion = "prototype-v1"

const maxIndexBytes = 64 * 1024 * 1024

type Availability string

const (
	Ready       Availability = "ready"
	OutsideRepo Availability = "outside_repository"
	NoStaged    Availability = "no_staged_changes"
	Conflicted  Availability = "conflicted_index"
	Unsupported Availability = "unsupported"
)

type State struct {
	Availability Availability `json:"availability"`
	Reason       string       `json:"reason,omitempty"`
	Root         string       `json:"root,omitempty"`
	RepoID       string       `json:"repository_id,omitempty"`
	WorktreeID   string       `json:"worktree_id,omitempty"`
	Head         string       `json:"head,omitempty"`
	IndexTree    string       `json:"index_tree,omitempty"`
	Config       string       `json:"config_version,omitempty"`
	Fingerprint  string       `json:"fingerprint,omitempty"`
}

func (s State) Scope() string { return s.RepoID + ":" + s.WorktreeID }

// Snapshot uses Git plumbing rather than timestamps. write-tree describes the
// exact index while symbolic-ref keeps distinct unborn branches distinct.
func Snapshot(ctx context.Context, cwd string) (State, error) {
	paths, err := git(ctx, cwd, "rev-parse", "--show-toplevel", "--path-format=absolute", "--git-common-dir", "--git-dir")
	if err != nil {
		return State{Availability: OutsideRepo, Reason: "outside a Git worktree"}, nil
	}
	pathParts := strings.Split(paths, "\n")
	if len(pathParts) != 3 {
		return State{}, fmt.Errorf("unexpected Git worktree identity output")
	}
	root := pathParts[0]
	root, err = canonical(root)
	if err != nil {
		return State{}, err
	}
	commonDir, gitDir := pathParts[1], pathParts[2]
	commonDir, err = canonical(commonDir)
	if err != nil {
		return State{}, err
	}
	gitDir, err = canonical(gitDir)
	if err != nil {
		return State{}, err
	}
	state := State{Availability: Ready, Root: root, RepoID: digest(commonDir), WorktreeID: digest(gitDir)}

	headOutput, err := git(ctx, cwd, "rev-parse", "HEAD", "HEAD^{tree}")
	headExists := err == nil
	headTree := ""
	if err != nil {
		branch, branchErr := git(ctx, cwd, "symbolic-ref", "-q", "HEAD")
		if branchErr != nil {
			branch = "detached-unborn"
		}
		state.Head = "unborn:" + branch
	} else {
		headParts := strings.Split(headOutput, "\n")
		if len(headParts) != 2 {
			return State{}, fmt.Errorf("unexpected HEAD identity output")
		}
		state.Head, headTree = headParts[0], headParts[1]
	}
	tree, err := writeTree(ctx, cwd, gitDir)
	if err != nil {
		unmerged, conflictErr := git(ctx, cwd, "ls-files", "-u")
		if conflictErr == nil && unmerged != "" {
			return State{Availability: Conflicted, Reason: "the index contains unresolved conflicts", Root: root, RepoID: state.RepoID, WorktreeID: state.WorktreeID}, nil
		}
		return State{}, fmt.Errorf("read exact index tree: %w", err)
	}
	state.IndexTree = tree

	if headExists && tree == headTree {
		state.Availability, state.Reason = NoStaged, "no staged changes"
		return state, nil
	}
	if !headExists {
		entries, err := git(ctx, cwd, "ls-files", "--cached", "--stage")
		if err != nil {
			return State{}, fmt.Errorf("inspect unborn index: %w", err)
		}
		if entries == "" {
			state.Availability, state.Reason = NoStaged, "no staged changes"
			return state, nil
		}
	}

	settings, err := config.Load()
	if err != nil {
		return State{Availability: Unsupported, Reason: err.Error(), Root: root, RepoID: state.RepoID, WorktreeID: state.WorktreeID}, nil
	}
	repoConfig, err := config.RepositoryVersion(root)
	if err != nil {
		return State{Availability: Unsupported, Reason: err.Error(), Root: root, RepoID: state.RepoID, WorktreeID: state.WorktreeID}, nil
	}
	state.Config = digest(settings.Version + "\x00" + repoConfig)
	state.Fingerprint = fingerprint(state.RepoID, state.WorktreeID, state.Head, state.IndexTree, GeneratorVersion, state.Config)
	return state, nil
}

// writeTree gives Git a private copy of the index because write-tree may add a
// cache-tree extension to the supplied index. The real index remains read-only.
func writeTree(ctx context.Context, cwd, gitDir string) (string, error) {
	temporaryDir, err := os.MkdirTemp("", "zsh-git-inlay-index-")
	if err != nil {
		return "", fmt.Errorf("create private index copy: %w", err)
	}
	defer os.RemoveAll(temporaryDir)
	indexPath := filepath.Join(temporaryDir, "index")
	if err := copyIndex(filepath.Join(gitDir, "index"), indexPath); err != nil {
		return "", err
	}
	return gitWithIndex(ctx, cwd, indexPath, "write-tree")
}

func copyIndex(sourcePath, destinationPath string) error {
	info, err := os.Lstat(sourcePath)
	if os.IsNotExist(err) {
		return nil // Git treats a missing alternate index as an empty index.
	}
	if err != nil {
		return fmt.Errorf("inspect Git index: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Git index is not a regular file")
	}
	if info.Size() > maxIndexBytes {
		return fmt.Errorf("Git index exceeds %d byte prototype limit", maxIndexBytes)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open Git index: %w", err)
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create private index copy: %w", err)
	}
	_, copyErr := io.Copy(destination, source)
	closeErr := destination.Close()
	if copyErr != nil {
		return fmt.Errorf("copy Git index: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close private index copy: %w", closeErr)
	}
	return nil
}

func git(ctx context.Context, cwd string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	command.Env = []string{"GIT_OPTIONAL_LOCKS=0"}
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(output), "\n"), nil
}

func gitWithIndex(ctx context.Context, cwd, indexPath string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	command.Env = []string{"GIT_OPTIONAL_LOCKS=0", "GIT_INDEX_FILE=" + indexPath}
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(output), "\n"), nil
}

func canonical(path string) (string, error) {
	path = strings.TrimSpace(path)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve Git path %q: %w", path, err)
	}
	return filepath.Abs(resolved)
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func fingerprint(parts ...string) string {
	var buffer bytes.Buffer
	for _, part := range parts {
		fmt.Fprintf(&buffer, "%d:%s", len(part), part)
	}
	return digest(buffer.String())
}

func WithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Second)
}
