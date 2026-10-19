package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/grounding"
	"github.com/gongahkia/zsh-git-inlay/internal/learning"
)

func TestPreferencesReachGroundedPublishedCandidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("ZSH_GIT_INLAY_CONFIG", path)
	if err := os.WriteFile(path, []byte("[commit]\ncapitalization = 'sentence'\nline_length = 40\nscope_mode = 'none'\n[suggestions]\ncandidate_count = 1\n[learning]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	repository := daemonRepository(t, "preferences.go")
	record, err := PrepareForEvaluation(context.Background(), settings, repository, filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Candidates) != 1 || len(record.Grounding) != 1 || record.Grounding[0].State != grounding.Grounded || record.Learning.Enabled {
		t.Fatalf("preferences not reflected in published record: %+v", record)
	}
	if message := record.Candidates[0].Message; !strings.HasPrefix(message, "chore: Add ") || len(message) > 40 {
		t.Fatalf("style was rejected instead of adapted: %q", message)
	}
}

func TestGlobalLearningDisablePreventsObservationAndReordering(t *testing.T) {
	server, _ := testServer(t)
	store, err := learning.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server.learner = store
	server.providerMu.Lock()
	server.preferences.LearningEnabled = false
	server.providerMu.Unlock()
	repository := daemonRepository(t, "no-learning.go")
	if reply := server.prepareLearning(repository); reply.Status != "ignored" {
		t.Fatalf("disabled learning prepared observation: %+v", reply)
	}
	state := daemonSnapshot(t, repository)
	profile, err := store.Load(state.RepoID)
	if err != nil || profile.Local.Samples != 0 {
		t.Fatalf("disabled learning retained observation: %+v %v", profile, err)
	}
	if server.matchesLearning(Record{Learning: learning.Explanation{Enabled: true}}) {
		t.Fatal("disabled learning accepted a record previously ranked using learning")
	}
}

func TestEvaluationUsesExplicitMessageDefaultsRatherThanAmbientDogfoodingPreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("ZSH_GIT_INLAY_CONFIG", path)
	if err := os.WriteFile(path, []byte("[commit]\ncapitalization = 'sentence'\nscope_mode = 'none'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := daemonRepository(t, "evaluation.go")
	record, err := PrepareForEvaluation(context.Background(), config.Default(), repository, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if record.Policy.Capitalization != "lower" || record.Policy.ScopeMode != "auto" {
		t.Fatalf("ambient preferences changed the explicit evaluation baseline: %+v", record.Policy)
	}
}
