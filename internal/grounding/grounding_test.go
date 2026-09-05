package grounding

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/provider"
	"github.com/gongahkia/zsh-git-inlay/internal/repoctx"
)

func TestEvaluateRejectsUnsupportedClaimsAndRanksGroundedFirst(t *testing.T) {
	compiled := groundedContext(t)
	results := Evaluate([]provider.Candidate{
		{Type: "fix", Scope: "parser", Subject: "prevent payment timeout ABC-999", EvidenceIDs: []string{"change:99"}},
		{Type: "test", Scope: "parser", Subject: "cover parser behavior ABC-123", EvidenceIDs: []string{"change:0"}},
		{Type: "docs", Subject: "update parser guidance", EvidenceIDs: []string{"change:0"}},
	}, compiled, config.DefaultRepositoryPolicy())
	if results[0].State != Ungrounded || len(results[0].UnsupportedClaims) < 2 {
		t.Fatalf("adversarial candidate = %#v", results[0])
	}
	if results[1].State != Grounded || results[2].State != Grounded {
		t.Fatalf("grounded candidates = %#v", results)
	}
	order := Rank(results, "conservative")
	if len(order) != 2 || order[0] != 2 || order[1] != 1 {
		t.Fatalf("conservative rank = %v results=%#v", order, results)
	}
	if visible := Rank(results, "visible"); len(visible) != 3 || visible[2] != 0 {
		t.Fatalf("visible rank = %v", visible)
	}
}

func TestEvaluateMarksHeuristicFixPartialAndQuietFiltersIt(t *testing.T) {
	compiled := groundedContext(t)
	results := Evaluate([]provider.Candidate{{Type: "fix", Scope: "parser", Subject: "fix parser behavior", EvidenceIDs: []string{"change:0"}}}, compiled, config.DefaultRepositoryPolicy())
	if results[0].State != PartiallyGrounded {
		t.Fatalf("fix result = %#v", results[0])
	}
	if quiet := Rank(results, "quiet"); len(quiet) != 0 {
		t.Fatalf("quiet retained partially grounded result: %v", quiet)
	}
	if conservative := Rank(results, "conservative"); len(conservative) != 1 {
		t.Fatalf("conservative omitted partial result: %v", conservative)
	}
}

func TestEvaluateAppliesRepositoryTypeScopePathAndWordingPolicy(t *testing.T) {
	compiled := groundedContext(t)
	policy := config.RepositoryPolicy{
		Convention:     "conventional",
		Types:          []string{"test"},
		Scopes:         []string{"parser"},
		ScopePaths:     []config.ScopeRule{{Path: "internal/parser", Scope: "parser"}},
		LineLength:     40,
		Capitalization: "lower",
		Body:           "optional",
	}
	results := Evaluate([]provider.Candidate{
		{Type: "test", Scope: "parser", Subject: "cover parser tests", EvidenceIDs: []string{"change:0"}},
		{Type: "docs", Scope: "parser", Subject: "cover parser tests", EvidenceIDs: []string{"change:0"}},
		{Type: "test", Scope: "other", Subject: "cover parser tests", EvidenceIDs: []string{"change:0"}},
		{Type: "test", Scope: "parser", Subject: "Cover parser tests", EvidenceIDs: []string{"change:0"}},
	}, compiled, policy)
	if results[0].State != Grounded {
		t.Fatalf("policy-compliant candidate = %#v", results[0])
	}
	for _, result := range results[1:] {
		if result.State != Ungrounded {
			t.Fatalf("repository policy did not reject candidate: %#v", result)
		}
	}
	requiredBody := policy
	requiredBody.Body = "required"
	if result := Evaluate([]provider.Candidate{{Type: "test", Scope: "parser", Subject: "cover parser tests", EvidenceIDs: []string{"change:0"}}}, compiled, requiredBody)[0]; result.State != Ungrounded || !EligibleForBodyComposition(result) {
		t.Fatalf("required body policy did not retain a safe compose input: %#v", result)
	}
	if EligibleForBodyComposition(results[1]) {
		t.Fatalf("type-invalid candidate was accepted for composition: %#v", results[1])
	}
	sentence := policy
	sentence.Capitalization = "sentence"
	if result := Evaluate([]provider.Candidate{{Type: "test", Scope: "parser", Subject: "Cover parser tests", EvidenceIDs: []string{"change:0"}}}, compiled, sentence)[0]; result.State != Grounded {
		t.Fatalf("sentence capitalization policy was not applied: %#v", result)
	}
}

func groundedContext(t *testing.T) repoctx.Compiled {
	t.Helper()
	repository := t.TempDir()
	groundingGit(t, repository, "init", "-q", "-b", "feature/ABC-123-parser")
	groundingGit(t, repository, "config", "user.name", "Test")
	groundingGit(t, repository, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	groundingGit(t, repository, "add", "README.md")
	groundingGit(t, repository, "commit", "-qm", "docs: bootstrap")
	if err := os.MkdirAll(filepath.Join(repository, "internal", "parser"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "internal", "parser", "parser_test.go"), []byte("package parser\nfunc TestParser(t any) {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	groundingGit(t, repository, "add", ".")
	state, err := gitstate.Snapshot(context.Background(), repository)
	if err != nil || state.Availability != gitstate.Ready {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	compiled, err := repoctx.Compile(context.Background(), repository, state, "deterministic")
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func groundingGit(t *testing.T, cwd string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
