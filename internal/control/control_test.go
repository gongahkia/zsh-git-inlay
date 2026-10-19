package control

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGlobalAndRepositoryRulesWithSnoozeExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "controls.json")
	id := strings.Repeat("a", 64)
	if _, err := Set(path, id, "snooze", time.Minute); err != nil {
		t.Fatal(err)
	}
	state, err := Load(path)
	if err != nil || state.Check(id, time.Now()).Status != "snoozed" || state.Check(id, time.Now().Add(2*time.Minute)).Status != "enabled" || state.Check(strings.Repeat("b", 64), time.Now()).Status != "enabled" {
		t.Fatalf("snooze scope/expiry: %+v %v", state, err)
	}
	if _, err := Set(path, "", "disable", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Set(path, id, "enable", 0); err != nil {
		t.Fatal(err)
	}
	decision, err := Check(path, id)
	if err != nil || decision.Status != "disabled" || decision.Scope != "global" {
		t.Fatalf("repository enable bypassed global disable: %+v %v", decision, err)
	}
	if _, err := Set(path, "", "enable", 0); err != nil {
		t.Fatal(err)
	}
	if decision, err := Check(path, id); err != nil || decision.Status != "enabled" {
		t.Fatalf("enable did not clear snooze/disable: %+v %v", decision, err)
	}
}

func TestControlStateIsPrivateAndCorruptionFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "controls.json")
	if _, err := Set(path, "", "snooze", 25*time.Hour); err == nil {
		t.Fatal("unbounded snooze accepted")
	}
	if _, err := Set(path, "", "disable", 0); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("controls are not private: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"schema":1,"global":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if decision, err := Check(path, ""); err == nil || decision.Status != "controls_invalid" {
		t.Fatalf("corrupt controls did not fail closed: %+v %v", decision, err)
	}
	if _, err := Set(path, "", "enable", 0); err == nil {
		t.Fatal("enable silently overwrote corrupt control state")
	}
}

func TestConcurrentRepositoryChangesAreNotLost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "controls.json")
	var group sync.WaitGroup
	failures := make(chan error, 2)
	for _, digit := range []string{"a", "b"} {
		group.Add(1)
		go func(digit string) {
			defer group.Done()
			_, err := Set(path, strings.Repeat(digit, 64), "disable", 0)
			failures <- err
		}(digit)
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	state, err := Load(path)
	if err != nil || len(state.Repositories) != 2 {
		t.Fatalf("concurrent controls were lost: %+v %v", state, err)
	}
}
