package daemon

import (
	"encoding/json"
	"testing"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
)

func TestDiagnosticsDistinguishUnpreparedFailedAndPolicyStates(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	repository := daemonRepository(t, "facts.go")
	server := New(config.Default(), "", t.TempDir())
	state := daemonSnapshot(t, repository)
	check := func(expected string) {
		t.Helper()
		var value Diagnostic
		if err := json.Unmarshal(server.diagnose(repository).Payload, &value); err != nil {
			t.Fatal(err)
		}
		if value.Status != expected || value.Action == "" || value.Fingerprint != state.Fingerprint {
			t.Fatalf("diagnostic=%#v", value)
		}
	}
	check("not_prepared")
	server.failures[state.Fingerprint] = "generate provider response: Ollama transport_or_timeout"
	check("generation_failed")
	server.failures[state.Fingerprint] = "no eligible candidate after grounding"
	check("withheld_by_policy")
	if len(server.jobs) != 0 {
		t.Fatal("diagnostics triggered generation")
	}
}
