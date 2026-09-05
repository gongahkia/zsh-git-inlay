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
	if err := os.WriteFile(path, []byte("[daemon]\nidle_timeout = \"50ms\"\nmax_active_repositories = 3\nmax_generation_concurrency = 1\n[cache]\nmax_records = 4\nmax_bytes = 65536\nmax_age = \"1h\"\n[provider]\nname = \"ollama\"\nmodel = \"qwen2.5-coder:0.5b\"\ntimeout = \"9s\"\nfallback = \"none\"\n[grounding]\nambiguity = \"quiet\"\n[activity]\nretention = \"2h\"\nmax_events = 4\n[zsh]\ncycle_keybinding = \"^Xh\"\n[diagnostics]\nverbose = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", root)
	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.IdleTimeout != 50*time.Millisecond || settings.MaxRepositories != 3 || settings.MaxGenerationJobs != 1 || settings.CacheMaxRecords != 4 || settings.CacheMaxBytes != 65536 || settings.CacheMaxAge != time.Hour || settings.Provider != "ollama" || settings.ProviderModel != "qwen2.5-coder:0.5b" || settings.ProviderTimeout != 9*time.Second || settings.ProviderFallback != "none" || settings.GroundingPolicy != "quiet" || settings.ActivityRetention != 2*time.Hour || settings.ActivityMaxEvents != 4 || settings.CycleKeybinding != "^Xh" || !settings.Verbose || settings.Version == "default" {
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
		"[provider]\nname = \"openai\"\nmodel = \"gpt-5\"\n",
		"[provider]\napi_key = \"not-allowed\"\n",
		"[provider]\nmodel = \"http://example.invalid/model\"\n",
		"[provider]\ntimeout = \"100ms\"\n",
		"[provider]\nfallback = \"cloud\"\n",
		"[grounding]\nambiguity = \"unsafe\"\n",
		"[activity]\nretention = \"20s\"\n",
		"[activity]\nmax_events = 4097\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(); err == nil {
			t.Fatalf("unsafe provider config accepted: %q", content)
		}
	}
}

func TestLoadAcceptsExplicitOpenAIWithoutCredentialConfiguration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "zsh-git-inlay", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[provider]\nname = \"openai\"\nmodel = \"gpt-5\"\ntimeout = \"9s\"\nfallback = \"none\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", root)
	settings, err := Load()
	if err != nil || settings.Provider != "openai" || settings.ProviderModel != "gpt-5" || settings.ProviderFallback != "none" {
		t.Fatalf("OpenAI settings=%#v err=%v", settings, err)
	}
}

func TestRepositoryConfigRejectsCapabilities(t *testing.T) {
	repository := t.TempDir()
	path := filepath.Join(repository, ".zsh-git-inlay.toml")
	if err := os.WriteFile(path, []byte("[commit]\nconvention = \"conventional\"\ntypes = [\"feat\", \"fix\"]\nscopes = [\"api\", \"cli\"]\nscope_paths = [\"internal/api=api\", \"cmd=cli\"]\nline_length = 72\ncapitalization = \"sentence\"\nbody = \"optional\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadRepositoryPolicy(repository)
	if err != nil || policy.Version == "none" || policy.Convention != "conventional" || policy.LineLength != 72 || policy.Capitalization != "sentence" || len(policy.Types) != 2 || len(policy.Scopes) != 2 || len(policy.ScopePaths) != 2 {
		t.Fatalf("policy = %#v, err = %v", policy, err)
	}
	for _, content := range []string{
		"[provider]\nendpoint = \"https://example.invalid\"\n",
		"[permissions]\nactivity = true\n",
		"[cloud]\nopenai = [\"staged_diff\"]\n",
		"[commit]\ntypes = 3\n",
		"[commit]\nscope_paths = [\"../secret=api\"]\n",
		"[commit]\nscopes = [\"api\"]\nscope_paths = [\"cmd=cli\"]\n",
		"[commit]\ncapitalization = \"run this\"\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadRepositoryPolicy(repository); err == nil {
			t.Fatalf("unsafe repository config accepted: %q", content)
		}
	}
}
