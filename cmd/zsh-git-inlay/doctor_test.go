package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/control"
)

func TestDoctorInspectionDoesNotCreateRuntimeOrHistory(t *testing.T) {
	dir, _ := adminFixture(t)
	output, _ := captureCommandOutput(func() error {
		return run([]string{"doctor", "--json", "--shell-file", filepath.Join(dir, ".zshrc"), "--autosuggestions", filepath.Join(dir, "dependency.zsh")})
	})
	var report struct{ Checks []doctorCheck }
	if err := json.Unmarshal([]byte(output), &report); err != nil || len(report.Checks) == 0 {
		t.Fatalf("missing structured checks: %s %v", output, err)
	}
	for _, key := range []string{"ZSH_GIT_INLAY_RUNTIME_DIR", "ZSH_GIT_INLAY_DATA_DIR", "ZSH_GIT_INLAY_STATE_DIR", "ZSH_GIT_INLAY_CACHE_DIR"} {
		if _, err := os.Lstat(os.Getenv(key)); !os.IsNotExist(err) {
			t.Fatalf("read-only doctor created %s: %v", key, err)
		}
	}
	found := false
	for _, check := range report.Checks {
		if check.Name == "shell_activation" {
			found = check.Status == "warning" && strings.Contains(check.Action, "--check")
		}
	}
	if !found {
		t.Fatal("inspection claimed interactive activation without executing it")
	}
}

func TestDoctorFixPreservesValidPreferencesAndRequiresExplicitReenableAfterCorruption(t *testing.T) {
	dir, args := adminFixture(t)
	t.Setenv("ZDOTDIR", dir) // Actual startup probes must use this disposable shell.
	if _, err := captureCommandOutput(func() error { return run(args) }); err != nil {
		t.Fatal(err)
	}
	broken := "# retain this choice\n[provider]\nname = 'openai' # no model or grant\n[suggestions]\ncandidate_count = 2\n"
	if err := os.MkdirAll(filepath.Dir(config.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.Path(), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	controlsPath, err := control.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controlsPath, []byte("broken control JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(dir, "prefix with spaces/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh")
	if err := os.WriteFile(plugin, []byte("# broken plugin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, commandErr := captureCommandOutput(func() error { return run([]string{"doctor", "--fix", "--json"}) })
	// These unit fixtures intentionally lack a real dependency/plugin. The
	// remaining activation failures must still return failure after repairs.
	if commandErr == nil {
		t.Fatal("doctor hid unresolved activation problems")
	}
	var report struct {
		Repairs []doctorRepair
		Checks  []doctorCheck
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil || len(report.Repairs) < 3 {
		t.Fatalf("repairs not recorded: %s %v", output, err)
	}
	settings, err := config.Load()
	if err != nil || settings.Provider != "deterministic" || settings.CandidateCount != 2 {
		t.Fatalf("repair lost a valid choice or enabled cloud: %+v %v", settings, err)
	}
	decision, err := control.Check(controlsPath, "")
	if err != nil || decision.Status != "disabled" {
		t.Fatalf("corruption silently re-enabled suggestions: %+v %v", decision, err)
	}
	data, _ := os.ReadFile(plugin)
	if string(data) != "# test plugin\n" {
		t.Fatalf("doctor did not restore verified plugin: %q", data)
	}
	for _, repair := range report.Repairs {
		if repair.Transaction != "" {
			if _, err := os.Stat(repair.Backup); err != nil {
				t.Fatalf("repair has no backup: %+v %v", repair, err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("ZSH_GIT_INLAY_DATA_DIR"), "cloud-grants.json")); !os.IsNotExist(err) {
		t.Fatal("doctor created an implicit cloud grant")
	}
}
