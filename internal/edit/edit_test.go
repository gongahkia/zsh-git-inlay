package edit

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
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

func TestInterruptedMergedUndoRecoversWithoutLosingLaterEdits(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "shell"), filepath.Join(dir, "binary")
	write(t, first, "original\n")
	store := Store{Dir: filepath.Join(dir, "journal")}
	journal, err := store.Apply("install", []Change{{Before: read(t, first), Data: []byte("original\nmanaged\n")}, {Before: read(t, second), Data: []byte("binary")}})
	if err != nil {
		t.Fatal(err)
	}
	write(t, first, "original\nmanaged\nlater edit\n")
	journal.State = "undoing"
	journal.UndoChanges = []Change{{Before: read(t, first), Data: []byte("original\nlater edit\n"), Mode: 0o640}, {Before: read(t, second), Remove: true}}
	if err := store.locked(func() error {
		if err := store.save(journal); err != nil {
			return err
		}
		return apply(journal.UndoChanges[0])
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Recover(); err != nil {
		t.Fatal(err)
	}
	if string(read(t, first).Data) != "original\nlater edit\n" || read(t, second).Exists {
		t.Fatal("recovery lost later edits or left partial undo")
	}
	if _, err := store.Undo(journal.ID); err != nil {
		t.Fatal(err)
	}
}

func TestInspectReadsMetadataWithoutCreatingOrDecodingBackups(t *testing.T) {
	dir := t.TempDir()
	store := Store{Dir: filepath.Join(dir, "history")}
	if headers, err := store.Inspect(); err != nil || len(headers) != 0 {
		t.Fatalf("empty history: %v", err)
	}
	if _, err := os.Stat(store.Dir); !os.IsNotExist(err) {
		t.Fatal("inspection created the history directory")
	}
	path := filepath.Join(dir, "data")
	journal, err := store.Apply("large backup", []Change{{Before: read(t, path), Data: []byte(strings.Repeat("private bytes", 10000))}})
	if err != nil {
		t.Fatal(err)
	}
	headers, err := store.Inspect()
	if err != nil || len(headers) != 1 || headers[0].ID != journal.ID || headers[0].State != "committed" {
		t.Fatalf("bounded metadata inspection: %+v %v", headers, err)
	}
}

func TestRealProcessDeathRecoversPreparedMultiFileTransaction(t *testing.T) {
	if os.Getenv("ZGI_EDIT_CRASH_HELPER") == "1" {
		dir := os.Getenv("ZGI_EDIT_CRASH_DIRECTORY")
		store := Store{Dir: filepath.Join(dir, "history"), afterWrite: func(index int) error {
			if index == 0 {
				if err := os.WriteFile(filepath.Join(dir, "written"), []byte("ready"), 0o600); err != nil {
					return err
				}
				for {
					time.Sleep(time.Second)
				}
			}
			return nil
		}}
		first, err := Read(filepath.Join(dir, "first"))
		if err != nil {
			os.Exit(2)
		}
		second, err := Read(filepath.Join(dir, "second"))
		if err != nil {
			os.Exit(2)
		}
		_, _ = store.Apply("crash rehearsal", []Change{{Before: first, Data: []byte("new")}, {Before: second, Data: []byte("created")}})
		os.Exit(3)
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "first"), "original")
	command := exec.Command(os.Args[0], "-test.run=^TestRealProcessDeathRecoversPreparedMultiFileTransaction$")
	command.Env = append(os.Environ(), "ZGI_EDIT_CRASH_HELPER=1", "ZGI_EDIT_CRASH_DIRECTORY="+dir)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "written")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("crash helper did not publish its first file")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	store := Store{Dir: filepath.Join(dir, "history")}
	headers, err := store.Inspect()
	if err != nil || len(headers) != 1 || headers[0].State != "prepared" {
		t.Fatalf("process death did not leave a recoverable intent: %+v %v", headers, err)
	}
	if err := store.Recover(); err != nil {
		t.Fatal(err)
	}
	if string(read(t, filepath.Join(dir, "first")).Data) != "original" || read(t, filepath.Join(dir, "second")).Exists {
		t.Fatal("process crash left a partial transaction")
	}
}

func TestReadRejectsPipeWithoutWaitingForAWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config-pipe")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := Read(path); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("named pipe was accepted as a regular file")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reader blocked waiting for a pipe writer")
	}
}
