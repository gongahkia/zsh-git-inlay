package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadValidatesGlobalSettings(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "zsh-git-inlay", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[daemon]\nidle_timeout = \"50ms\"\nmax_active_repositories = 3\nmax_generation_concurrency = 1\n[zsh]\ncycle_keybinding = \"^Xh\"\n[diagnostics]\nverbose = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", root)
	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.IdleTimeout != 50*time.Millisecond || settings.MaxRepositories != 3 || settings.MaxGenerationJobs != 1 || settings.CycleKeybinding != "^Xh" || !settings.Verbose || settings.Version == "default" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestRepositoryConfigRejectsCapabilities(t *testing.T) {
	repository := t.TempDir()
	path := filepath.Join(repository, ".zsh-git-inlay.toml")
	if err := os.WriteFile(path, []byte("[commit]\nconvention = \"conventional\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if version, err := RepositoryVersion(repository); err != nil || version == "none" {
		t.Fatalf("version = %q, err = %v", version, err)
	}
	if err := os.WriteFile(path, []byte("[provider]\nendpoint = \"https://example.invalid\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RepositoryVersion(repository); err == nil {
		t.Fatal("provider capability was accepted")
	}
	if err := os.WriteFile(path, []byte("[commit]\ntypes = 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RepositoryVersion(repository); err == nil {
		t.Fatal("invalid repository schema was accepted")
	}
}
