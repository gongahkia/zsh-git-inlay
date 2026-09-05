package gitstate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotTracksExactStagedState(t *testing.T) {
	repository := newRepository(t, true)
	write(t, repository, "src/app.txt", "one\n")
	gitRun(t, repository, "add", "src/app.txt")
	first := snapshot(t, repository)
	if first.Availability != Ready {
		t.Fatalf("availability = %s (%s)", first.Availability, first.Reason)
	}

	write(t, repository, "src/app.txt", "unstaged only\n")
	if got := snapshot(t, repository).Fingerprint; got != first.Fingerprint {
		t.Fatalf("unstaged edit changed fingerprint: %s != %s", got, first.Fingerprint)
	}

	write(t, repository, "src/other.txt", "two\n")
	gitRun(t, repository, "add", "src/other.txt")
	second := snapshot(t, repository)
	if second.Fingerprint == first.Fingerprint {
		t.Fatal("staging another path did not change fingerprint")
	}
	gitRun(t, repository, "restore", "--staged", "src/other.txt")
	third := snapshot(t, repository)
	if third.Fingerprint != first.Fingerprint {
		t.Fatal("unstaging did not restore the exact staged-state fingerprint")
	}
}

func TestSnapshotHandlesDeletionRenameAndMode(t *testing.T) {
	repository := newRepository(t, true)
	write(t, repository, "old.txt", "old\n")
	write(t, repository, "script", "#!/bin/sh\n")
	gitRun(t, repository, "add", ".")
	gitRun(t, repository, "commit", "-qm", "initial")
	baseline := snapshot(t, repository)

	gitRun(t, repository, "mv", "old.txt", "new.txt")
	if err := os.Chmod(filepath.Join(repository, "script"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "add", "-u")
	changed := snapshot(t, repository)
	if changed.Fingerprint == baseline.Fingerprint {
		t.Fatal("rename and mode change did not change fingerprint")
	}
	gitRun(t, repository, "restore", "--staged", ".")
	gitRun(t, repository, "reset", "--", ".")
	gitRun(t, repository, "checkout", "--", ".")
	os.Remove(filepath.Join(repository, "old.txt"))
	gitRun(t, repository, "add", "-u")
	if got := snapshot(t, repository).Fingerprint; got == baseline.Fingerprint {
		t.Fatal("deletion did not change fingerprint")
	}
}

func TestSnapshotHeadAndUnbornIdentity(t *testing.T) {
	unborn := newRepository(t, false)
	write(t, unborn, "new.txt", "new\n")
	gitRun(t, unborn, "add", "new.txt")
	state := snapshot(t, unborn)
	if state.Availability != Ready || state.Head != "unborn:refs/heads/main" {
		t.Fatalf("unborn state = %#v", state)
	}

	repository := newRepository(t, true)
	write(t, repository, "file", "one\n")
	gitRun(t, repository, "add", "file")
	before := snapshot(t, repository)
	gitRun(t, repository, "commit", "-qm", "one")
	write(t, repository, "file", "two\n")
	gitRun(t, repository, "add", "file")
	after := snapshot(t, repository)
	if before.Head == after.Head || before.Fingerprint == after.Fingerprint {
		t.Fatal("HEAD change did not invalidate the staged fingerprint")
	}
}

func TestSnapshotIsolatesRepositoriesAndLinkedWorktrees(t *testing.T) {
	firstRepo := newRepository(t, true)
	secondRepo := newRepository(t, true)
	write(t, firstRepo, "same.txt", "same\n")
	write(t, secondRepo, "same.txt", "same\n")
	gitRun(t, firstRepo, "add", "same.txt")
	gitRun(t, secondRepo, "add", "same.txt")
	first, second := snapshot(t, firstRepo), snapshot(t, secondRepo)
	if first.RepoID == second.RepoID || first.Fingerprint == second.Fingerprint {
		t.Fatal("separate repositories collided")
	}

	gitRun(t, firstRepo, "commit", "-qm", "base")
	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, firstRepo, "worktree", "add", "-qb", "linked", linked)
	write(t, firstRepo, "main.txt", "main\n")
	write(t, linked, "linked.txt", "linked\n")
	gitRun(t, firstRepo, "add", "main.txt")
	gitRun(t, linked, "add", "linked.txt")
	mainState, linkedState := snapshot(t, firstRepo), snapshot(t, linked)
	if mainState.RepoID != linkedState.RepoID || mainState.WorktreeID == linkedState.WorktreeID || mainState.Fingerprint == linkedState.Fingerprint {
		t.Fatalf("linked worktree isolation failed: main=%#v linked=%#v", mainState, linkedState)
	}
}

func TestSnapshotRejectsConflict(t *testing.T) {
	repository := newRepository(t, true)
	write(t, repository, "conflict.txt", "base\n")
	gitRun(t, repository, "add", ".")
	gitRun(t, repository, "commit", "-qm", "base")
	gitRun(t, repository, "checkout", "-qb", "other")
	write(t, repository, "conflict.txt", "other\n")
	gitRun(t, repository, "commit", "-am", "other")
	gitRun(t, repository, "checkout", "-q", "main")
	write(t, repository, "conflict.txt", "main\n")
	gitRun(t, repository, "commit", "-am", "main")
	command := exec.Command("git", "-C", repository, "merge", "other")
	if err := command.Run(); err == nil {
		t.Fatal("expected merge conflict")
	}
	if state := snapshot(t, repository); state.Availability != Conflicted {
		t.Fatalf("availability = %s (%s)", state.Availability, state.Reason)
	}
}

func TestSnapshotNoStagedAndOutsideRepository(t *testing.T) {
	repository := newRepository(t, true)
	if state := snapshot(t, repository); state.Availability != NoStaged {
		t.Fatalf("availability = %s", state.Availability)
	}
	if state := snapshot(t, t.TempDir()); state.Availability != OutsideRepo {
		t.Fatalf("availability = %s", state.Availability)
	}
}

func TestSnapshotSupportsConcurrentReaders(t *testing.T) {
	repository := newRepository(t, false)
	write(t, repository, "file.txt", "staged\n")
	gitRun(t, repository, "add", "file.txt")
	errors := make(chan error, 16)
	for worker := 0; worker < cap(errors); worker++ {
		go func() {
			context, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			state, err := Snapshot(context, repository)
			if err == nil && state.Availability != Ready {
				err = fmt.Errorf("availability = %s", state.Availability)
			}
			errors <- err
		}()
	}
	for worker := 0; worker < cap(errors); worker++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
}

func TestSnapshotDoesNotModifyIndexEntries(t *testing.T) {
	repository := newRepository(t, false)
	write(t, repository, "file.txt", "staged\n")
	gitRun(t, repository, "add", "file.txt")
	index := filepath.Join(repository, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	_ = snapshot(t, repository)
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("snapshot changed the Git index")
	}
}

func snapshot(t *testing.T, cwd string) State {
	t.Helper()
	context, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state, err := Snapshot(context, cwd)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func newRepository(t *testing.T, withCommit bool) string {
	t.Helper()
	repository := t.TempDir()
	gitRun(t, repository, "init", "-q", "-b", "main")
	gitRun(t, repository, "config", "user.email", "test@example.invalid")
	gitRun(t, repository, "config", "user.name", "Test")
	if withCommit {
		write(t, repository, "README.md", "base\n")
		gitRun(t, repository, "add", "README.md")
		gitRun(t, repository, "commit", "-qm", "initial")
	}
	return repository
}

func write(t *testing.T, root, name, value string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, cwd string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
