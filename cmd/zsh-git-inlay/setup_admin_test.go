package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
)

func adminFixture(t *testing.T) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	for key, value := range map[string]string{
		"ZSH_GIT_INLAY_CONFIG":      filepath.Join(dir, "config/zsh-git-inlay/config.toml"),
		"ZSH_GIT_INLAY_DATA_DIR":    filepath.Join(dir, "data/zsh-git-inlay"),
		"ZSH_GIT_INLAY_STATE_DIR":   filepath.Join(dir, "state/zsh-git-inlay"),
		"ZSH_GIT_INLAY_CACHE_DIR":   filepath.Join(dir, "cache/zsh-git-inlay"),
		"ZSH_GIT_INLAY_RUNTIME_DIR": filepath.Join(dir, "run/zsh-git-inlay"),
	} {
		t.Setenv(key, value)
	}
	source := filepath.Join(dir, "source")
	if err := os.MkdirAll(filepath.Join(source, ".build"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{filepath.Join(source, ".build/zsh-git-inlay"): "#!/bin/sh\nexit 0\n", filepath.Join(source, "zsh-git-inlay.plugin.zsh"): "# test plugin\n", filepath.Join(source, "THIRD_PARTY_NOTICES"): "test notice\n", filepath.Join(dir, "dependency.zsh"): "# test dependency\n"} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return dir, []string{"setup", "install", "--source", source, "--prefix", filepath.Join(dir, "prefix with spaces"), "--shell-file", filepath.Join(dir, ".zshrc"), "--autosuggestions", filepath.Join(dir, "dependency.zsh"), "--skip-shell-check", "--json"}
}

func TestAdminInstallTuneRemoveAndRestore(t *testing.T) {
	dir, args := adminFixture(t)
	args = append(args, "--set", "commit.capitalization=sentence", "--set", "suggestions.candidate_count=2")
	output, err := captureCommandOutput(func() error { return run(args) })
	if err != nil {
		t.Fatalf("install: %s %v", output, err)
	}
	settings, err := config.Load()
	if err != nil || settings.MessageCaps != "sentence" || settings.CandidateCount != 2 {
		t.Fatalf("wizard choices did not apply: %+v %v", settings, err)
	}
	output, err = captureCommandOutput(func() error { return run(args) })
	if err != nil || !strings.Contains(output, `"changed": false`) {
		t.Fatalf("repeat setup: %s %v", output, err)
	}
	output, err = captureCommandOutput(func() error { return run([]string{"uninstall", "--json"}) })
	if err != nil {
		t.Fatalf("remove: %s %v", output, err)
	}
	var removed struct {
		Transaction    string
		RecoveryHelper string `json:"recovery_helper"`
	}
	if err := json.Unmarshal([]byte(output), &removed); err != nil || removed.Transaction == "" || removed.RecoveryHelper == "" {
		t.Fatalf("removal has no usable recovery: %s %v", output, err)
	}
	if info, err := os.Stat(removed.RecoveryHelper); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("private recovery helper: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prefix with spaces/bin/zsh-git-inlay")); !os.IsNotExist(err) {
		t.Fatal("remove left installed executable")
	}
	if _, err := config.Load(); err != nil {
		t.Fatalf("remove discarded preferences: %v", err)
	}
	if _, err := captureCommandOutput(func() error { return run([]string{"undo", removed.Transaction}) }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prefix with spaces/bin/zsh-git-inlay")); err != nil {
		t.Fatalf("undo did not restore installed executable: %v", err)
	}
	if _, err := captureCommandOutput(func() error { return run([]string{"uninstall", "--purge-local-data", "--json"}) }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(os.Getenv("ZSH_GIT_INLAY_DATA_DIR")); !os.IsNotExist(err) {
		t.Fatal("explicit purge left local data")
	}
	if _, err := os.Stat(removed.RecoveryHelper); !os.IsNotExist(err) {
		t.Fatal("explicit purge retained the recovery binary")
	}
}

func TestInteractiveCustomizationCancelsWithoutWritingThenSavesReviewedChoices(t *testing.T) {
	_, _ = adminFixture(t)
	answers := []string{"customize", "deterministic", "2", "72", "sentence", "none", "^Xh", "false", "no", "no"}
	var output bytes.Buffer
	if err := setupAdmin("wizard", nil, strings.NewReader(strings.Join(answers, "\n")+"\n"), &output); err == nil {
		t.Fatal("unapproved wizard applied changes")
	}
	if _, err := os.Stat(config.Path()); !os.IsNotExist(err) {
		t.Fatal("cancelled wizard wrote preferences")
	}
	answers[len(answers)-1] = "yes"
	if err := setupAdmin("wizard", nil, strings.NewReader(strings.Join(answers, "\n")+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load()
	if err != nil || settings.CandidateCount != 2 || settings.MessageCaps != "sentence" || settings.MessageLineLength != 72 || settings.ScopeMode != "none" || settings.CycleKeybinding != "^Xh" || settings.LearningEnabled {
		t.Fatalf("reviewed choices not saved: %+v %v", settings, err)
	}
}

func TestInvalidProviderBatchLeavesInstallationUntouched(t *testing.T) {
	dir, args := adminFixture(t)
	args = append(args, "--set", "provider.name=openai")
	if _, err := captureCommandOutput(func() error { return run(args) }); err == nil {
		t.Fatal("incomplete provider selection installed")
	}
	if _, err := os.Stat(filepath.Join(dir, "prefix with spaces/bin/zsh-git-inlay")); !os.IsNotExist(err) {
		t.Fatal("invalid provider selection partly installed")
	}
}

func TestSetupRepairUsesVerifiedBackupInsteadOfTheBrokenInstalledPlugin(t *testing.T) {
	dir, args := adminFixture(t)
	if _, err := captureCommandOutput(func() error { return run(args) }); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(dir, "prefix with spaces/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh")
	if err := os.WriteFile(plugin, []byte("# user-broken plugin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := captureCommandOutput(func() error { return run([]string{"setup", "repair", "--skip-shell-check", "--json"}) }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(plugin)
	if err != nil || string(data) != "# test plugin\n" {
		t.Fatalf("repair blessed the corrupt source instead of restoring a verified copy: %q %v", data, err)
	}
}
