package config

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestTOMLValuesAndCommentPreservingEdits(t *testing.T) {
	content := []byte("# preferences\n[zsh]\ncycle_keybinding = '\\e#' # keep this\ncycle_keymaps = [\n  'emacs', # primary\n  'viins',\n]\n[provider]\nname = 'deterministic'\n")
	settings, err := Parse(content, "preferences.toml")
	if err != nil || settings.CycleKeybinding != `\e#` || strings.Join(settings.CycleKeymaps, ",") != "emacs,viins" {
		t.Fatalf("TOML strings/arrays were corrupted: %+v, %v", settings, err)
	}
	updated, err := Edit(content, "preferences.toml", "provider.timeout", "12s", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(updated, content) || !bytes.Contains(updated, []byte("timeout = \"12s\"")) {
		t.Fatalf("insertion changed existing bytes: %s", updated)
	}
	updated, err = Edit(updated, "preferences.toml", "zsh.cycle_keybinding", "^X#", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(updated, []byte(`cycle_keybinding = "^X#" # keep this`)) || !bytes.Contains(updated, []byte("'emacs', # primary")) {
		t.Fatalf("set lost comments: %s", updated)
	}
	updated, err = Edit(updated, "preferences.toml", "provider.timeout", "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	settings, err = Parse(updated, "preferences.toml")
	if err != nil || settings.ProviderTimeout != Default().ProviderTimeout {
		t.Fatalf("reset did not restore default: %v", err)
	}
}

func TestTypedShowIncludesDefaultEffectiveAndOrigin(t *testing.T) {
	entries, err := Show([]byte("[provider]\ntimeout = '12s'\n"), "/tmp/prefs.toml", false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Key == "provider.timeout" {
			found = true
			if entry.Effective != "12s" || entry.Default != "8s" || entry.Source != "/tmp/prefs.toml" || entry.Line != 2 || entry.Type != "duration" {
				t.Fatalf("wrong setting provenance: %+v", entry)
			}
		}
	}
	if !found || len(entries) != len(settingsEntries(Default())) {
		t.Fatalf("schema missing settings: %d", len(entries))
	}
}

func TestValidationHasPathLineKeyAndPreservesInputOnError(t *testing.T) {
	content := []byte("# private\n[ollama]\ncontext_tokens = 2\n")
	_, err := Parse(content, "/tmp/config.toml")
	var diagnostic *Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.Path != "/tmp/config.toml" || diagnostic.Line != 3 || diagnostic.Key != "ollama.context_tokens" {
		t.Fatalf("missing location: %v", err)
	}
	if _, err := Edit(content, "/tmp/config.toml", "provider.timeout", "not a duration", false, false); err == nil {
		t.Fatal("invalid edit passed validation")
	}
	if string(content) != "# private\n[ollama]\ncontext_tokens = 2\n" {
		t.Fatal("validation modified caller bytes")
	}
}

func TestRepairQuarantinesOnlyInvalidPreferences(t *testing.T) {
	content := []byte("# keep my choices\n[provider]\nname = 'ollama' # model is absent\n[ollama]\ncontext_tokens = 2 # invalid\noutput_tokens = 512 # useful\n[zsh]\ncycle_keybinding = '^X#'\nunknown = 'typo'\n")
	repair, err := RepairDocument(content, "preferences.toml", false)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := Parse(repair.Content, "preferences.toml")
	if err != nil {
		t.Fatal(err)
	}
	if settings.Provider != "deterministic" || settings.OllamaOutputTokens != 512 || settings.CycleKeybinding != "^X#" || settings.OllamaContextTokens != Default().OllamaContextTokens {
		t.Fatalf("repair lost valid choices or enabled a provider: %+v\n%s", settings, repair.Content)
	}
	for _, expected := range []string{"# keep my choices", "output_tokens = 512 # useful", "# name = 'ollama' # model is absent", "# context_tokens = 2 # invalid", "# unknown = 'typo'"} {
		if !bytes.Contains(repair.Content, []byte(expected)) {
			t.Fatalf("repair dropped original text %q:\n%s", expected, repair.Content)
		}
	}
	if len(repair.Diagnostics) != 3 {
		t.Fatalf("expected three explained repairs, got %+v", repair.Diagnostics)
	}
	again, err := RepairDocument(repair.Content, "preferences.toml", false)
	if err != nil || !bytes.Equal(again.Content, repair.Content) || len(again.Diagnostics) != 0 {
		t.Fatalf("repair was not idempotent: %v", err)
	}
}

func TestRepairSyntaxAndRepositoryCapabilities(t *testing.T) {
	content := []byte("[zsh]\ncycle_keybinding = 'broken\ncycle_keymaps = ['viins'] # valid\n")
	repair, err := RepairDocument(content, "prefs.toml", false)
	if err != nil || !bytes.Contains(repair.Content, []byte("cycle_keymaps = ['viins'] # valid")) {
		t.Fatalf("could not repair unterminated string without losing valid settings: %v\n%s", err, repair.Content)
	}
	if _, err := ParseRepository([]byte("[provider]\nname = 'openai'\n"), "repo.toml"); err == nil {
		t.Fatal("repository granted a provider capability")
	}
	if _, err := Edit(nil, "repo.toml", "provider.name", "openai", false, true); err == nil {
		t.Fatal("repository editor accepted a provider capability")
	}
}

func TestInsertionWithDottedKeysAndExistingSections(t *testing.T) {
	for _, input := range []string{"provider.name = 'deterministic'\n", "[zsh]\ncycle_keybinding = '^Xg'\n[provider]\nname = 'deterministic'"} {
		updated, err := Edit([]byte(input), "prefs.toml", "provider.timeout", "10s", false, false)
		if err != nil {
			t.Fatalf("dotted/table insertion: %v\n%s", err, updated)
		}
	}
}

func TestTOMLTypesRemainStrict(t *testing.T) {
	for _, input := range []string{
		"[diagnostics]\nverbose = 1\n",
		"[provider]\ntimeout = 8\n",
		"[zsh]\ncycle_keymaps = ['emacs', 1]\n",
		"[provider]\nname = 'deterministic'\nname = 'ollama'\n",
		"[unknown]\n",
	} {
		if _, err := Parse([]byte(input), "prefs.toml"); err == nil {
			t.Fatalf("accepted invalid/ambiguous TOML: %s", input)
		}
	}
}

func TestRepairNeverReassignsUnknownTableChildren(t *testing.T) {
	content := []byte("[provider]\nname = 'deterministic'\n[unrecognized]\nmodel = 'gpt-example'\nfallback = 'none'\n[zsh]\ncycle_keybinding = '^Xp'\n")
	repair, err := RepairDocument(content, "prefs.toml", false)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := Parse(repair.Content, "prefs.toml")
	if err != nil || settings.ProviderModel != "" || settings.ProviderFallback != "deterministic" || settings.CycleKeybinding != "^Xp" {
		t.Fatalf("repair reassigned unknown table's children: %+v %v\n%s", settings, err, repair.Content)
	}
}

func TestBatchConfigValidatesProviderAndModelAsOneChange(t *testing.T) {
	content := []byte("# retain me\n[zsh]\ncycle_keybinding = '^Xg'\n")
	updated, err := EditMany(content, "prefs.toml", map[string]string{"provider.name": "openai", "provider.model": "gpt-example", "provider.fallback": "none", "commit.capitalization": "sentence"}, false)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := Parse(updated, "prefs.toml")
	if err != nil || settings.Provider != "openai" || settings.ProviderModel != "gpt-example" || settings.ProviderFallback != "none" || settings.MessageCaps != "sentence" || !bytes.Contains(updated, []byte("# retain me")) {
		t.Fatalf("batch failed to preserve/validate preferences: %+v %v", settings, err)
	}
	if _, err := EditMany(content, "prefs.toml", map[string]string{"provider.name": "openai"}, false); err == nil {
		t.Fatal("incomplete provider batch passed validation")
	}
}
