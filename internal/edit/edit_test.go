package edit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func read(t *testing.T, path string) Snapshot {
	t.Helper()
	snapshot, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestSymlinkAndModeSurviveApplyAndUndo(t *testing.T) {
	dir := t.TempDir()
	target, link := filepath.Join(dir, "dotfiles-zshrc"), filepath.Join(dir, ".zshrc")
	write(t, target, "# personal config\n")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	store := Store{Dir: filepath.Join(dir, "journal")}
	journal, err := store.Apply("install", []Change{{Before: read(t, link), Data: []byte("# personal config\n# managed\n")}})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("dotfile link was replaced: %v", err)
	}
	if _, err := store.Undo(journal.ID); err != nil {
		t.Fatal(err)
	}
	snapshot := read(t, link)
	if string(snapshot.Data) != "# personal config\n" || snapshot.Mode != 0o640 {
		t.Fatalf("undo changed data/mode: %+v", snapshot)
	}
	if _, err := store.Undo(journal.ID); err != nil {
		t.Fatalf("repeat undo: %v", err)
	}
}

func TestInterruptedMultiFileEditRollsBack(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	write(t, first, "original")
	store := Store{Dir: filepath.Join(dir, "journal"), afterWrite: func(int) error { return errors.New("interrupted") }}
	_, err := store.Apply("install", []Change{{Before: read(t, first), Data: []byte("changed")}, {Before: read(t, second), Data: []byte("created")}})
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("expected interruption: %v", err)
	}
	if string(read(t, first).Data) != "original" || read(t, second).Exists {
		t.Fatal("partial transaction survived rollback")
	}
}

func TestCrashJournalRecoveredOnNextOperation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	write(t, path, "old")
	store := Store{Dir: filepath.Join(dir, "journal")}
	change := Change{Before: read(t, path), Data: []byte("new")}
	// Model process death between a successful rename and the final commit.
	err := store.locked(func() error {
		if err := store.save(Journal{ID: "crash", State: "prepared", Changes: []Change{change}}); err != nil {
			return err
		}
		return apply(change)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Recover(); err != nil {
		t.Fatal(err)
	}
	if string(read(t, path).Data) != "old" {
		t.Fatal("crash backup was not restored")
	}
	if err := store.Recover(); err != nil {
		t.Fatal(err)
	}
}

func TestUndoPreservesLaterEditsAcrossTransaction(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	store := Store{Dir: filepath.Join(dir, "journal")}
	journal, err := store.Apply("install", []Change{{Before: read(t, first), Data: []byte("managed")}, {Before: read(t, second), Data: []byte("managed")}})
	if err != nil {
		t.Fatal(err)
	}
	write(t, second, "user edit")
	if _, err := store.Undo(journal.ID); err == nil || !strings.Contains(err.Error(), "later edits are preserved") {
		t.Fatalf("expected useful conflict: %v", err)
	}
	if string(read(t, first).Data) != "managed" || string(read(t, second).Data) != "user edit" {
		t.Fatal("undo overwrote a later edit or partly undid the transaction")
	}
}

func TestConcurrentEditorsCannotOverwriteEachOther(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	snapshot := read(t, path)
	store := Store{Dir: filepath.Join(dir, "journal")}
	var group sync.WaitGroup
	errors := make(chan error, 2)
	for _, value := range []string{"a", "b"} {
		group.Add(1)
		go func(value string) {
			defer group.Done()
			_, err := store.Apply("set", []Change{{Before: snapshot, Data: []byte(value)}})
			errors <- err
		}(value)
	}
	group.Wait()
	close(errors)
	successes := 0
	for err := range errors {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("%d writes succeeded, expected exactly one", successes)
	}
}

func TestRetargetedOrDanglingSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	first, second, link := filepath.Join(dir, "first"), filepath.Join(dir, "second"), filepath.Join(dir, "link")
	write(t, first, "same")
	write(t, second, "same")
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	snapshot := read(t, link)
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	_, err := (Store{Dir: filepath.Join(dir, "journal")}).Apply("set", []Change{{Before: snapshot, Data: []byte("changed")}})
	if err == nil {
		t.Fatal("retargeted symlink accepted")
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "dangling")); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(filepath.Join(dir, "dangling")); err == nil {
		t.Fatal("dangling symlink accepted")
	}
}
