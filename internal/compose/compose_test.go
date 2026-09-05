package compose

import (
	"strings"
	"testing"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/repoctx"
)

func TestProposeWrapsOnlyGroundedPathFactsWithoutRepeatingSubject(t *testing.T) {
	policy := config.DefaultRepositoryPolicy()
	policy.LineLength = 24
	evidence := []repoctx.Evidence{{ID: "change:0", Status: "M", Path: "internal/very-long-component-name/file.go"}, {ID: "change:1", Status: "A", Path: "docs/guide.md"}}
	plan, err := Propose("docs(repo): update guide", evidence, policy)
	if err != nil || !plan.Grounding.Grounded || len(plan.Facts) != 2 || strings.Contains(strings.ToLower(plan.Body), strings.ToLower(plan.Subject)) {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if err := Validate(plan, evidence, policy); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(plan.Body, "\n") {
		if len([]rune(line)) > policy.LineLength {
			t.Fatalf("unwrapped line %q", line)
		}
	}
}

func TestProposeAndEditedValidationRespectBodyPolicy(t *testing.T) {
	policy := config.DefaultRepositoryPolicy()
	evidence := []repoctx.Evidence{{ID: "change:0", Status: "M", Path: "file.go"}}
	policy.Body = "forbid"
	if _, err := Propose("chore(repo): update file", evidence, policy); err == nil {
		t.Fatal("forbidden body was proposed")
	}
	policy.Body = "required"
	if err := ValidateEdited("chore(repo): update file\n", policy); err == nil {
		t.Fatal("required body was accepted as empty")
	}
	if err := ValidateEdited("chore(repo): update file\n\nUser-authored rationale.\n", policy); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEdited("chore(repo): update file\n\nchore(repo): update file\n", policy); err == nil {
		t.Fatal("edited duplicate subject was accepted")
	}
	policy.LineLength = 20
	if err := ValidateEdited("chore(repo): update\n\nThis line is intentionally far too long.\n", policy); err == nil {
		t.Fatal("unwrapped edited body was accepted")
	}
}

func TestProposeRejectsUngroundedOrDuplicateFacts(t *testing.T) {
	policy := config.DefaultRepositoryPolicy()
	evidence := []repoctx.Evidence{{ID: "change:0", Status: "M", Path: "file.go"}}
	plan, err := Propose("chore(repo): update file", evidence, policy)
	if err != nil {
		t.Fatal(err)
	}
	plan.Facts[0].Path = "other.go"
	if err := Validate(plan, evidence, policy); err == nil {
		t.Fatal("ungrounded body fact was accepted")
	}
	duplicate := Plan{Subject: "chore(repo): update file", Body: "chore(repo): update file", Facts: []Fact{{EvidenceID: "change:0", Status: "M", Path: "file.go"}}}
	if err := Validate(duplicate, evidence, policy); err == nil {
		t.Fatal("subject duplicated generated body heading")
	}
}
