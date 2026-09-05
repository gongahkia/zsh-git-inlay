// Package compose builds and validates a bounded, user-editable commit body.
package compose

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/repoctx"
)

const (
	MaxFacts       = 12
	MaxMessageSize = 16 * 1024
)

// Fact is a deterministic body assertion tied to one exact staged evidence ID.
// Paths are shown only in the user-selected editor file and never persisted by
// this package.
type Fact struct {
	EvidenceID string `json:"evidence_id"`
	Status     string `json:"status"`
	Path       string `json:"path"`
}

type Grounding struct {
	Grounded    bool     `json:"grounded"`
	EvidenceIDs []string `json:"evidence_ids"`
	Reason      string   `json:"reason"`
}

type Plan struct {
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	Facts     []Fact    `json:"facts"`
	Grounding Grounding `json:"grounding"`
}

func (plan Plan) Message() string { return plan.Subject + "\n\n" + plan.Body + "\n" }

func ReadMessage(path string) (string, error) { return readMessage(path) }

// Propose creates only a factual staged-path list. It deliberately does not
// infer effects, test outcomes, or motivations for a commit body.
func Propose(subject string, evidence []repoctx.Evidence, policy config.RepositoryPolicy) (Plan, error) {
	if policy.Body == "forbid" {
		return Plan{}, fmt.Errorf("repository policy forbids commit bodies")
	}
	if err := validSubject(subject, policy); err != nil {
		return Plan{}, err
	}
	facts := make([]Fact, 0, MaxFacts)
	for _, item := range evidence {
		if len(facts) == MaxFacts {
			break
		}
		if item.ID == "" || item.Status == "" || item.Path == "" || strings.ContainsAny(item.Status, "\r\n\x00") {
			continue
		}
		facts = append(facts, Fact{EvidenceID: item.ID, Status: item.Status, Path: item.Path})
	}
	if len(facts) == 0 {
		return Plan{}, fmt.Errorf("no staged path evidence is available for a body")
	}
	plan := Plan{Subject: subject, Facts: facts}
	lines := []string{"Staged paths:"}
	for _, fact := range facts {
		lines = append(lines, wrap("- "+fact.Status+" "+quotePath(fact.Path), policy.LineLength)...)
	}
	plan.Body = strings.Join(lines, "\n")
	if err := Validate(plan, evidence, policy); err != nil {
		return Plan{}, err
	}
	identifiers := make([]string, len(facts))
	for index, fact := range facts {
		identifiers[index] = fact.EvidenceID
	}
	plan.Grounding = Grounding{Grounded: true, EvidenceIDs: identifiers, Reason: "each generated body path/status is copied from exact staged evidence"}
	return plan, nil
}

// Validate verifies every generated factual body element before an editor sees
// it. User edits remain user-authored and are intentionally not relabeled as
// model-grounded claims.
func Validate(plan Plan, evidence []repoctx.Evidence, policy config.RepositoryPolicy) error {
	if err := validSubject(plan.Subject, policy); err != nil {
		return err
	}
	if plan.Body == "" || len(plan.Body) > MaxMessageSize || strings.Contains(plan.Body, "\x00") || strings.Contains(strings.ToLower(plan.Body), strings.ToLower(plan.Subject)) {
		return fmt.Errorf("invalid or duplicate generated commit body")
	}
	available := map[string]repoctx.Evidence{}
	for _, item := range evidence {
		available[item.ID] = item
	}
	seen := map[string]bool{}
	for _, fact := range plan.Facts {
		item, found := available[fact.EvidenceID]
		if !found || seen[fact.EvidenceID] || fact.Status != item.Status || fact.Path != item.Path {
			return fmt.Errorf("body fact is not grounded in staged evidence")
		}
		seen[fact.EvidenceID] = true
	}
	if len(plan.Facts) == 0 {
		return fmt.Errorf("generated body has no facts")
	}
	for _, line := range strings.Split(plan.Body, "\n") {
		if utf8.RuneCountInString(line) > policy.LineLength {
			return fmt.Errorf("generated body line exceeds repository line length")
		}
	}
	return nil
}

// ValidateEdited applies only repository body-shape policy after the editor.
// It preserves user prose rather than attempting to misrepresent it as a
// generated, grounded claim.
func ValidateEdited(message string, policy config.RepositoryPolicy) error {
	if len(message) == 0 || len(message) > MaxMessageSize || strings.Contains(message, "\x00") {
		return fmt.Errorf("edited message is empty or exceeds the compose limit")
	}
	normalized := strings.ReplaceAll(message, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || validSubject(strings.TrimSuffix(lines[0], "\r"), policy) != nil {
		return fmt.Errorf("edited message has an invalid subject")
	}
	body := strings.TrimSpace(strings.TrimPrefix(normalized, lines[0]))
	if policy.Body == "forbid" && body != "" {
		return fmt.Errorf("repository policy forbids commit bodies")
	}
	if policy.Body == "required" && body == "" {
		return fmt.Errorf("repository policy requires a commit body")
	}
	if body != "" && strings.Contains(strings.ToLower(body), strings.ToLower(lines[0])) {
		return fmt.Errorf("edited commit body duplicates its subject")
	}
	for _, line := range strings.Split(body, "\n") {
		if utf8.RuneCountInString(strings.TrimSuffix(line, "\r")) > policy.LineLength {
			return fmt.Errorf("edited body line exceeds repository line length")
		}
	}
	return nil
}

func validSubject(subject string, policy config.RepositoryPolicy) error {
	if subject == "" || strings.ContainsAny(subject, "\r\n\x00") || utf8.RuneCountInString(subject) > policy.LineLength {
		return fmt.Errorf("invalid commit subject")
	}
	return nil
}

func quotePath(path string) string {
	var builder strings.Builder
	builder.WriteByte('`')
	for _, character := range path {
		switch character {
		case '`':
			builder.WriteString("\\`")
		case '\\':
			builder.WriteString("\\\\")
		case '\n':
			builder.WriteString("\\n")
		case '\r':
			builder.WriteString("\\r")
		case '\t':
			builder.WriteString("\\t")
		default:
			if character < 0x20 || character == 0x7f {
				fmt.Fprintf(&builder, "\\u%04x", character)
			} else {
				builder.WriteRune(character)
			}
		}
	}
	builder.WriteByte('`')
	return builder.String()
}

func wrap(value string, width int) []string {
	if width < 1 {
		return []string{value}
	}
	runes := []rune(value)
	result := make([]string, 0, len(runes)/width+1)
	for len(runes) > width {
		cut := width
		for index := width; index > 0; index-- {
			if runes[index-1] == ' ' {
				cut = index - 1
				break
			}
		}
		if cut == 0 {
			cut = width
		}
		result = append(result, string(runes[:cut]))
		runes = runes[cut:]
		for len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	if len(runes) > 0 {
		result = append(result, string(runes))
	}
	return result
}
