package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCLIRepairSetResetUndoAndLaterEditConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	t.Setenv("ZSH_GIT_INLAY_CONFIG", path)
	t.Setenv("ZSH_GIT_INLAY_STATE_DIR", filepath.Join(dir, "state"))
	original := "# personal\n[provider]\ntimeout = 'nonsense'\n[zsh]\ncycle_keybinding = '^X#' # keep\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := captureCommandOutput(func() error { return run([]string{"config", "validate", "--json"}) })
	var validation struct {
		Diagnostic struct {
			Line int
			Key  string
		}
	}
	if decodeErr := json.Unmarshal([]byte(output), &validation); err == nil || decodeErr != nil || validation.Diagnostic.Line != 3 || validation.Diagnostic.Key != "provider.timeout" {
		t.Fatalf("invalid config was not diagnosed: %s %v", output, err)
	}
	output, err = captureCommandOutput(func() error { return run([]string{"config", "repair", "--json"}) })
	if err != nil {
		t.Fatal(err)
	}
	var report struct{ Transaction string }
	if err := json.Unmarshal([]byte(output), &report); err != nil || report.Transaction == "" {
		t.Fatalf("no recovery transaction: %s %v", output, err)
	}
	if _, err := captureCommandOutput(func() error { return run([]string{"undo", report.Transaction}) }); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatal("undo did not restore original malformed config")
	}
	for _, args := range [][]string{{"config", "set", "provider.timeout", "10s", "--json"}, {"config", "reset", "provider.timeout", "--json"}} {
		output, err = captureCommandOutput(func() error { return run(args) })
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if err := os.WriteFile(path, append(data, []byte("# later user edit\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureCommandOutput(func() error { return run([]string{"undo", report.Transaction}) }); err == nil || !strings.Contains(err.Error(), "later edits are preserved") {
		t.Fatalf("undo did not preserve later edits: %v", err)
	}
}
