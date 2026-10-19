// Package edit applies recoverable, compare-before-write changes to user files.
// Journals contain private backups; a later edit is never silently overwritten.
package edit

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const maxFileBytes = 64 << 20

type Snapshot struct {
	Path   string      `json:"path"`
	Target string      `json:"target"`
	Exists bool        `json:"exists"`
	Mode   os.FileMode `json:"mode"`
	Data   []byte      `json:"data"`
}

type Change struct {
	Before Snapshot    `json:"before"`
	Data   []byte      `json:"data,omitempty"`
	Mode   os.FileMode `json:"mode"`
	Remove bool        `json:"remove,omitempty"`
}

type Journal struct {
	ID      string    `json:"id"`
	Label   string    `json:"label"`
	Created time.Time `json:"created"`
	State   string    `json:"state"`
	Changes []Change  `json:"changes"`
}

type Store struct {
	Dir string
	// afterWrite is a test-only interruption point. No environment-controlled
	// fault injection is exposed by production commands.
	afterWrite func(int) error
}

// UpdatePrivate atomically updates one application-owned private state file.
// Unlike user-file edits it retains no backup, so bounded feedback/history can
// actually be cleared. fn runs under a process-wide advisory directory lock.
func UpdatePrivate(path string, fn func([]byte) ([]byte, error)) error {
	store := Store{Dir: filepath.Dir(path)}
	return store.locked(func() error {
		snapshot, err := Read(path)
		if err != nil {
			return err
		}
		info, statErr := os.Lstat(snapshot.Path)
		if snapshot.Exists && (statErr != nil || info.Mode()&os.ModeSymlink != 0 || snapshot.Mode.Perm()&0o077 != 0) {
			return fmt.Errorf("state file must be private and not a symlink: %s", path)
		}
		data, err := fn(snapshot.Data)
		if err != nil {
			return err
		}
		return apply(Change{Before: snapshot, Data: data, Mode: 0o600})
	})
}

// Read follows an existing file symlink, and records both names so replacing or
// retargeting that symlink is detected before the write. Atomic writes replace
// the target file, never the symlink in a dotfiles checkout.
func Read(path string) (Snapshot, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Snapshot{}, err
	}
	target, err := resolve(absolute)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{Path: absolute, Target: target, Mode: 0o600}
	file, err := os.Open(target)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Snapshot{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return Snapshot{}, fmt.Errorf("%s must be a regular file of at most %d bytes", path, maxFileBytes)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Getuid() {
		return Snapshot{}, fmt.Errorf("%s is not owned by this user", path)
	}
	snapshot.Data, err = io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil || len(snapshot.Data) > maxFileBytes {
		return Snapshot{}, fmt.Errorf("cannot read bounded file %s: %w", path, err)
	}
	snapshot.Exists, snapshot.Mode = true, info.Mode().Perm()
	return snapshot, nil
}

func resolve(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	// A dangling final symlink is ambiguous: do not create its missing target.
	if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s is a dangling symlink", path)
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	base, err := resolve(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, filepath.Base(path)), nil
}

func (store Store) Apply(label string, changes []Change) (Journal, error) {
	var result Journal
	err := store.locked(func() error {
		if err := store.recover(); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, change := range changes {
			if seen[change.Before.Target] || len(change.Data) > maxFileBytes {
				return fmt.Errorf("duplicate target or oversized change: %s", change.Before.Path)
			}
			seen[change.Before.Target] = true
			if err := matches(change.Before); err != nil {
				return err
			}
		}
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		result = Journal{ID: time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(random[:]), Label: label, Created: time.Now().UTC(), State: "prepared", Changes: changes}
		if err := store.save(result); err != nil {
			return err
		}
		for i, change := range changes {
			if err := apply(change); err != nil {
				return errors.Join(err, store.rollback(&result))
			}
			if store.afterWrite != nil {
				if err := store.afterWrite(i); err != nil {
					return errors.Join(err, store.rollback(&result))
				}
			}
		}
		result.State = "committed"
		// A failed journal sync intentionally leaves a recoverable prepared
		// transaction. Report failure instead of claiming the edit committed.
		if err := store.save(result); err != nil {
			return errors.Join(err, store.rollback(&result))
		}
		return nil
	})
	return result, err
}

func (store Store) Undo(id string) (Journal, error) {
	var result Journal
	err := store.locked(func() error {
		if err := store.recover(); err != nil {
			return err
		}
		journal, err := store.read(id)
		if err != nil {
			return err
		}
		result = journal
		if journal.State == "undone" {
			return nil
		}
		if journal.State != "committed" {
			return fmt.Errorf("transaction %s is %s", id, journal.State)
		}
		// Check the entire transaction first. An edit in any file prevents a
		// partial undo; the backup remains available for manual reconciliation.
		for _, change := range journal.Changes {
			if err := matches(after(change)); err != nil {
				return fmt.Errorf("cannot undo %s; later edits are preserved: %w", id, err)
			}
		}
		journal.State = "undoing"
		if err := store.save(journal); err != nil {
			return err
		}
		err = store.rollback(&journal)
		result = journal
		return err
	})
	return result, err
}

func (store Store) Recover() error { return store.locked(store.recover) }

func (store Store) List() ([]Journal, error) {
	var journals []Journal
	err := store.locked(func() error {
		entries, err := os.ReadDir(store.Dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			journal, err := store.read(strings.TrimSuffix(entry.Name(), ".json"))
			if err != nil {
				return err
			}
			journals = append(journals, journal)
		}
		sort.Slice(journals, func(i, j int) bool { return journals[i].Created.After(journals[j].Created) })
		return nil
	})
	return journals, err
}

func (store Store) recover() error {
	entries, err := os.ReadDir(store.Dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		journal, err := store.read(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return err
		}
		if journal.State == "prepared" || journal.State == "undoing" {
			if err := store.rollback(&journal); err != nil {
				return fmt.Errorf("recover transaction %s from %s: %w", journal.ID, store.Dir, err)
			}
		}
	}
	return nil
}

func (store Store) rollback(journal *Journal) error {
	// Recover only writes whose contents still match our published version.
	// Never discard an intervening editor save, even after an interrupted run.
	for _, change := range journal.Changes {
		if matches(change.Before) != nil {
			if err := matches(after(change)); err != nil {
				return fmt.Errorf("recovery conflict, preserve %s and inspect backup %s: %w", change.Before.Path, journal.ID, err)
			}
		}
	}
	for i := len(journal.Changes) - 1; i >= 0; i-- {
		change := journal.Changes[i]
		if matches(change.Before) == nil {
			continue
		}
		reverse := Change{Before: after(change), Data: change.Before.Data, Mode: change.Before.Mode, Remove: !change.Before.Exists}
		if err := apply(reverse); err != nil {
			return err
		}
	}
	journal.State = "undone"
	return store.save(*journal)
}

func after(change Change) Snapshot {
	mode := change.Mode.Perm()
	if mode == 0 {
		mode = change.Before.Mode
	}
	return Snapshot{Path: change.Before.Path, Target: change.Before.Target, Exists: !change.Remove, Mode: mode, Data: change.Data}
}

func matches(expected Snapshot) error {
	current, err := Read(expected.Path)
	if err != nil {
		return err
	}
	if current.Target != expected.Target || current.Exists != expected.Exists || (current.Exists && (!bytes.Equal(current.Data, expected.Data) || current.Mode != expected.Mode)) {
		return fmt.Errorf("%s changed since it was read", expected.Path)
	}
	return nil
}

func apply(change Change) error {
	if err := matches(change.Before); err != nil {
		return err
	}
	if change.Remove {
		if !change.Before.Exists {
			return nil
		}
		if err := os.Remove(change.Before.Target); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(change.Before.Target))
	}
	next := after(change)
	return atomicWriteChecked(next.Target, next.Data, next.Mode, func() error { return matches(change.Before) })
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	return atomicWriteChecked(path, data, mode, nil)
}

func atomicWriteChecked(path string, data []byte, mode os.FileMode, check func() error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".zgi-edit-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(mode.Perm()); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (store Store) locked(fn func() error) error {
	if err := os.MkdirAll(store.Dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(store.Dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("journal directory must be private and not a symlink: %s", store.Dir)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("journal directory is not owned by this user: %s", store.Dir)
	}
	file, err := os.OpenFile(filepath.Join(store.Dir, ".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return fn()
}

func (store Store) save(journal Journal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(store.Dir, journal.ID+".json"), data, 0o600)
}

func (store Store) read(id string) (Journal, error) {
	if id == "" || filepath.Base(id) != id || strings.ContainsAny(id, "/\\.") {
		return Journal{}, fmt.Errorf("invalid transaction ID %q", id)
	}
	file, err := os.OpenFile(filepath.Join(store.Dir, id+".json"), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return Journal{}, err
	}
	defer file.Close()
	var journal Journal
	err = json.NewDecoder(io.LimitReader(file, 256<<20)).Decode(&journal)
	if err == nil && journal.ID != id {
		err = fmt.Errorf("transaction ID does not match its journal")
	}
	return journal, err
}
