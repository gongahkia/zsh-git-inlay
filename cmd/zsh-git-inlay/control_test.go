package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalDisableSnoozeAndSessionControlsDoNotNeedAWorkingConfigOrDaemon(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZSH_GIT_INLAY_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("ZSH_GIT_INLAY_RUNTIME_DIR", filepath.Join(dir, "runtime"))
	t.Setenv("ZSH_GIT_INLAY_CONFIG", filepath.Join(dir, "broken.toml"))
	t.Setenv("ZSH_GIT_INLAY_DISABLED", "0")
	if err := os.WriteFile(filepath.Join(dir, "broken.toml"), []byte("broken config"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"disable", "--json"}, {"enable", "--json"}, {"snooze", "1h", "--json"}} {
		output, err := captureCommandOutput(func() error { return run(args) })
		if err != nil {
			t.Fatalf("control should not depend on config: %s %v", output, err)
		}
		if args[0] != "enable" {
			output, err = captureCommandOutput(func() error { return run([]string{"suggest", "--buffer", "git commit -m ", "--json"}) })
			expected := "disabled"
			if args[0] == "snooze" {
				expected = "snoozed"
			}
			if err != nil || !strings.Contains(output, expected) {
				t.Fatalf("disabled lookup tried broken configuration/daemon: %s %v", output, err)
			}
		}
	}
	t.Setenv("ZSH_GIT_INLAY_DISABLED", "1")
	output, err := captureCommandOutput(func() error { return run([]string{"control", "status", "--json"}) })
	if err != nil || !strings.Contains(output, "session") {
		t.Fatalf("session override missing: %s %v", output, err)
	}
}
