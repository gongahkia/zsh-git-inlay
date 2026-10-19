// Package lifecycle manages only explicitly recorded artifacts and one marked
// shell block. Every install, upgrade and removal uses private undo backups.
package lifecycle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/edit"
)

type Artifact struct {
	Hash   string      `json:"sha256"`
	Mode   os.FileMode `json:"mode"`
	Target string      `json:"target"`
}

type Manifest struct {
	Schema          int                 `json:"schema"`
	Prefix          string              `json:"prefix"`
	ShellFile       string              `json:"shell_file"`
	Autosuggestions string              `json:"autosuggestions"`
	Version         string              `json:"version"`
	Installed       time.Time           `json:"installed"`
	Files           map[string]Artifact `json:"files"`
}

type Options struct {
	Prefix, ShellFile, Autosuggestions string
	Binary, Plugin, Version            string
	Notices                            map[string]string // filename to local source path
	MigrateLegacy, Adopt, Repair       bool
	Configuration                      *edit.Change
}

type Manager struct {
	DataDir string
	History edit.Store
}

type Result struct {
	Changed  bool         `json:"changed"`
	Journal  edit.Journal `json:"-"`
	Manifest Manifest     `json:"installation"`
}

func hash(content []byte) string { sum := sha256.Sum256(content); return hex.EncodeToString(sum[:]) }

func (manager Manager) ManifestPath() string {
	return filepath.Join(manager.DataDir, "installation.json")
}

func (manager Manager) Load() (Manifest, error) {
	snapshot, err := edit.Read(manager.ManifestPath())
	if err != nil {
		return Manifest{}, err
	}
	if !snapshot.Exists {
		return Manifest{}, os.ErrNotExist
	}
	info, statErr := os.Lstat(manager.ManifestPath())
	if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
		return Manifest{}, errors.New("installation manifest may not be a symlink")
	}
	if len(snapshot.Data) > 1<<20 || snapshot.Mode.Perm()&0o077 != 0 {
		return Manifest{}, errors.New("installation manifest must be private and at most 1 MiB")
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("invalid installation manifest: %w", err)
	}
	if manifest.Schema != 1 || !filepath.IsAbs(manifest.Prefix) || manifest.Prefix == "/" || !filepath.IsAbs(manifest.ShellFile) || len(manifest.Files) > 4 || len(manifest.Files) < 2 || decoder.Decode(new(any)) != io.EOF {
		return manifest, errors.New("invalid installation manifest metadata")
	}
	allowed := artifactPaths(manifest.Prefix)
	for _, path := range []string{filepath.Join(manifest.Prefix, "bin/zsh-git-inlay"), filepath.Join(manifest.Prefix, "share/zsh-git-inlay/zsh-git-inlay.plugin.zsh")} {
		if _, exists := manifest.Files[path]; !exists {
			return manifest, errors.New("manifest does not track both required artifacts")
		}
	}
	for path, artifact := range manifest.Files {
		mode, known := allowed[path]
		_, hashErr := hex.DecodeString(artifact.Hash)
		if !known || artifact.Mode != mode || len(artifact.Hash) != 64 || hashErr != nil || !filepath.IsAbs(artifact.Target) {
			return manifest, errors.New("manifest contains an unowned artifact")
		}
	}
	return manifest, nil
}

func artifactPaths(prefix string) map[string]os.FileMode {
	return map[string]os.FileMode{
		filepath.Join(prefix, "bin/zsh-git-inlay"):                            0o755,
		filepath.Join(prefix, "share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"): 0o644,
		filepath.Join(prefix, "share/zsh-git-inlay/LICENSE"):                  0o644,
		filepath.Join(prefix, "share/zsh-git-inlay/THIRD_PARTY_NOTICES"):      0o644,
	}
}

func (manager Manager) Install(options Options) (Result, error) {
	var result Result
	if err := manager.History.Recover(); err != nil {
		return result, err
	}
	for _, value := range []string{options.Prefix, options.ShellFile, options.Autosuggestions, options.Binary, options.Plugin} {
		if value == "" || !filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\r\n") {
			return result, errors.New("installation paths must be nonempty absolute paths without control characters")
		}
	}
	if filepath.Clean(options.Prefix) == "/" {
		return result, errors.New("refusing the root install prefix")
	}
	options.Prefix, options.ShellFile = filepath.Clean(options.Prefix), filepath.Clean(options.ShellFile)
	dependency, err := os.Stat(options.Autosuggestions)
	if err != nil || !dependency.Mode().IsRegular() {
		return result, fmt.Errorf("zsh-autosuggestions must be a readable regular file: %s", options.Autosuggestions)
	}
	previous, previousErr := manager.Load()
	if previousErr != nil && !os.IsNotExist(previousErr) {
		return result, previousErr
	}
	if previousErr == nil && previous.Prefix != options.Prefix {
		return result, fmt.Errorf("installation already uses %s; uninstall it before changing prefix", previous.Prefix)
	}
	manifest := Manifest{Schema: 1, Prefix: options.Prefix, ShellFile: options.ShellFile, Autosuggestions: options.Autosuggestions, Version: options.Version, Installed: time.Now().UTC(), Files: map[string]Artifact{}}
	sources := map[string]string{filepath.Join(options.Prefix, "bin/zsh-git-inlay"): options.Binary, filepath.Join(options.Prefix, "share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"): options.Plugin}
	for name, source := range options.Notices {
		if name != "LICENSE" && name != "THIRD_PARTY_NOTICES" {
			return result, fmt.Errorf("unsupported installation notice %q", name)
		}
		sources[filepath.Join(options.Prefix, "share/zsh-git-inlay", name)] = source
	}
	changes := []edit.Change{}
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		source, err := readSource(sources[path])
		if err != nil || len(source) == 0 {
			return result, fmt.Errorf("read installation source %s: %w", sources[path], errors.Join(err, errors.New("source must be a nonempty regular user-owned file")))
		}
		if err := rejectArtifactLink(path); err != nil {
			return result, err
		}
		before, err := edit.Read(path)
		if err != nil {
			return result, err
		}
		owned, known := previous.Files[path]
		if known && before.Target != owned.Target {
			return result, fmt.Errorf("installed target for %s moved; restore its original prefix before upgrade", path)
		}
		if before.Exists && !options.Repair && !options.Adopt && ((!known && !bytes.Equal(before.Data, source)) || (known && hash(before.Data) != owned.Hash)) {
			return result, fmt.Errorf("preserve modified/untracked file %s; inspect it before explicit adoption or repair", path)
		}
		mode := artifactPaths(options.Prefix)[path]
		manifest.Files[path] = Artifact{Hash: hash(source), Mode: mode, Target: before.Target}
		changes = appendChanged(changes, edit.Change{Before: before, Data: source, Mode: mode})
	}
	// Notices from a previous release remain owned and retained if this source
	// snapshot omits them. Never erase a selected license during upgrade.
	for path, artifact := range previous.Files {
		if _, exists := manifest.Files[path]; !exists {
			manifest.Files[path] = artifact
		}
	}
	if previousErr == nil && previous.ShellFile != options.ShellFile {
		before, err := edit.Read(previous.ShellFile)
		if err != nil {
			return result, err
		}
		updated, err := RemoveBlock(before.Data)
		if err != nil {
			return result, err
		}
		changes = appendChanged(changes, edit.Change{Before: before, Data: updated, ManagedBlock: true})
	}
	shell, err := edit.Read(options.ShellFile)
	if err != nil {
		return result, err
	}
	updated, err := ReplaceBlock(shell.Data, Block(options.Prefix, options.Autosuggestions), options.MigrateLegacy)
	if err != nil {
		return result, err
	}
	changes = appendChanged(changes, edit.Change{Before: shell, Data: updated, ManagedBlock: true})
	if options.Configuration != nil {
		if options.Configuration.Remove {
			return result, errors.New("setup cannot remove user preferences")
		}
		if err := config.Validate(options.Configuration.Data, options.Configuration.Before.Path, false); err != nil {
			return result, err
		}
		changes = appendChanged(changes, *options.Configuration)
	}
	if previousErr == nil {
		manifest.Installed = previous.Installed
	}
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	before, err := edit.Read(manager.ManifestPath())
	if err != nil {
		return result, err
	}
	changes = appendChanged(changes, edit.Change{Before: before, Data: manifestData, Mode: 0o600})
	result.Manifest = manifest
	if len(changes) == 0 {
		return result, nil
	}
	result.Journal, err = manager.History.Apply("setup install/upgrade", changes)
	result.Changed = err == nil
	return result, err
}

func appendChanged(changes []edit.Change, change edit.Change) []edit.Change {
	if !change.Remove && !change.Before.Exists && len(change.Data) == 0 {
		return changes
	}
	mode := change.Mode
	if mode == 0 {
		mode = change.Before.Mode
	}
	if !change.Remove && change.Before.Exists && mode == change.Before.Mode && bytes.Equal(change.Before.Data, change.Data) {
		return changes
	}
	if change.Remove && !change.Before.Exists {
		return changes
	}
	return append(changes, change)
}

func (manager Manager) Uninstall() (Result, error) {
	var result Result
	if err := manager.History.Recover(); err != nil {
		return result, err
	}
	manifest, err := manager.Load()
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Manifest = manifest
	changes := []edit.Change{}
	shell, err := edit.Read(manifest.ShellFile)
	if err != nil {
		return result, err
	}
	updated, err := RemoveBlock(shell.Data)
	if err != nil {
		return result, err
	}
	changes = appendChanged(changes, edit.Change{Before: shell, Data: updated, ManagedBlock: true})
	paths := make([]string, 0, len(manifest.Files))
	for path := range manifest.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := rejectArtifactLink(path); err != nil {
			return result, err
		}
		before, err := edit.Read(path)
		if err != nil {
			return result, err
		}
		if before.Target != manifest.Files[path].Target || (before.Exists && hash(before.Data) != manifest.Files[path].Hash) {
			return result, fmt.Errorf("preserve modified managed artifact %s; restore it or move it before uninstall", path)
		}
		changes = appendChanged(changes, edit.Change{Before: before, Remove: true})
	}
	before, err := edit.Read(manager.ManifestPath())
	if err != nil {
		return result, err
	}
	changes = append(changes, edit.Change{Before: before, Remove: true})
	result.Journal, err = manager.History.Apply("setup uninstall", changes)
	result.Changed = err == nil
	return result, err
}

func rejectArtifactLink(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("installed artifact %s is a symlink; preserve the external target and inspect it before installation/removal", path)
	}
	return nil
}

func readSource(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return nil, errors.New("source must be a regular file of at most 64 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
	if len(data) > 64<<20 {
		return nil, errors.New("installation source exceeded 64 MiB")
	}
	return data, err
}
