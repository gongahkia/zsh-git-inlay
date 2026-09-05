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
	if err := os.WriteFile(path, []byte("[daemon]\nidle_timeout = \"50ms\"\nmax_active_repositories = 3\nmax_generation_concurrency = 1\n[cache]\nmax_records = 4\nmax_bytes = 65536\nmax_age = \"1h\"\n[provider]\nname = \"ollama\"\nmodel = \"qwen2.5-coder:0.5b\"\ntimeout = \"9s\"\nfallback = \"none\"\n[grounding]\nambiguity = \"quiet\"\n[zsh]\ncycle_keybinding = \"^Xh\"\n[diagnostics]\nverbose = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", root)
	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.IdleTimeout != 50*time.Millisecond || settings.MaxRepositories != 3 || settings.MaxGenerationJobs != 1 || settings.CacheMaxRecords != 4 || settings.CacheMaxBytes != 65536 || settings.CacheMaxAge != time.Hour || settings.Provider != "ollama" || settings.ProviderModel != "qwen2.5-coder:0.5b" || settings.ProviderTimeout != 9*time.Second || settings.ProviderFallback != "none" || settings.GroundingPolicy != "quiet" || settings.CycleKeybinding != "^Xh" || !settings.Verbose || settings.Version == "default" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestLoadRejectsUnsafeProviderConfiguration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "zsh-git-inlay", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", root)
	for _, content := range []string{
		"[provider]\nname = \"ollama\"\n",
		"[provider]\nname = \"cloud\"\n",
		"[provider]\nmodel = \"http://example.invalid/model\"\n",
		"[provider]\ntimeout = \"100ms\"\n",
		"[provider]\nfallback = \"cloud\"\n",
		"[grounding]\nambiguity = \"unsafe\"\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(); err == nil {
			t.Fatalf("unsafe provider config accepted: %q", content)
		}
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
