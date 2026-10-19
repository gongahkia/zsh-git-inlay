package provider

import (
	"strings"
	"testing"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/repoctx"
)

func TestPreferencesAdaptCapitalizationScopesAndLengthWithoutClaims(t *testing.T) {
	evidence := []repoctx.Evidence{{ID: "change:0", Path: "internal/api/router.go", Status: "M"}}
	policy := config.DefaultRepositoryPolicy()
	policy.Capitalization, policy.LineLength = "sentence", 32
	policy.Scopes = []string{"api"}
	policy.ScopePaths = []config.ScopeRule{{Path: "internal/api", Scope: "api"}}
	response := Response{Candidates: []Candidate{{Type: "chore", Scope: "internal", Subject: "update a very long and specific staged router declaration", EvidenceIDs: []string{"change:0"}}}}
	adapted := AdaptPreferences(response, evidence, policy)
	if len(adapted.Candidates) != 1 || adapted.Candidates[0].Subject != "Update router.go" || adapted.Candidates[0].Scope != "api" || adapted.Candidates[0].Type != "chore" {
		t.Fatalf("preferences did not adapt actual generation: %+v", adapted)
	}
	if response.Candidates[0].Scope != "internal" || !strings.HasPrefix(response.Candidates[0].Subject, "update a very") {
		t.Fatal("adaptation mutated the provider's original response")
	}
	policy.ScopeMode = "none"
	if value := AdaptPreferences(response, evidence, policy).Candidates[0]; value.Scope != "" {
		t.Fatalf("scope disabled: %+v", value)
	}
}

func TestPreferencesNeverInventEvidenceOrRewriteTypeAllowlist(t *testing.T) {
	policy := config.DefaultRepositoryPolicy()
	policy.LineLength, policy.Types = 24, []string{"fix"}
	response := Response{Candidates: []Candidate{{Type: "chore", Subject: "update an extremely long subject that does not fit", EvidenceIDs: []string{"change:99"}}}}
	adapted := AdaptPreferences(response, []repoctx.Evidence{{ID: "change:0", Path: "api.go"}}, policy)
	if adapted.Candidates[0].Type != "chore" || adapted.Candidates[0].EvidenceIDs[0] != "change:99" || adapted.Candidates[0].Subject != response.Candidates[0].Subject {
		t.Fatalf("preferences invented a claim: %+v", adapted)
	}
}
