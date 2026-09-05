// Package grounding ranks provider proposals against bounded staged evidence.
package grounding

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/provider"
	"github.com/gongahkia/zsh-git-inlay/internal/repoctx"
)

type State string

const (
	Grounded            State = "GROUNDED"
	PartiallyGrounded   State = "PARTIALLY_GROUNDED"
	Ungrounded          State = "UNGROUNDED"
	InsufficientContext State = "INSUFFICIENT_CONTEXT"
)

type Check struct {
	Name          string `json:"name"`
	Deterministic bool   `json:"deterministic"`
	Passed        bool   `json:"passed"`
	Detail        string `json:"detail"`
}

// Result contains structural evidence decisions, never staged source content.
type Result struct {
	State             State    `json:"state"`
	Score             int      `json:"score"`
	EvidenceIDs       []string `json:"evidence_ids"`
	Checks            []Check  `json:"checks"`
	UnsupportedClaims []string `json:"unsupported_claims,omitempty"`
}

var (
	issuePattern       = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,9}-[0-9]{1,7}\b`)
	wordPattern        = regexp.MustCompile(`[a-z0-9]{3,}`)
	testClaimPattern   = regexp.MustCompile(`\b(test|tests|testing|coverage|spec)\b`)
	fixClaimPattern    = regexp.MustCompile(`\b(fix|fixed|fixes|resolve|resolved|repair|prevent|avoid|eliminate|correct)\b`)
	effectClaimPattern = regexp.MustCompile(`\b(prevent|avoid|eliminate|ensure|guarantee|improve|support|enable|disable)\b`)
)

var conventionalTypes = map[string]bool{
	"build": true, "chore": true, "ci": true, "docs": true, "feat": true,
	"fix": true, "perf": true, "refactor": true, "revert": true, "style": true,
	"test": true,
}

// Evaluate applies deterministic structural checks first and reports heuristic
// signals separately. It does not claim semantic proof of a behavioral change.
func Evaluate(candidates []provider.Candidate, compiled repoctx.Compiled, policy config.RepositoryPolicy) []Result {
	evidence := map[string]repoctx.Evidence{"metadata:staged": {ID: "metadata:staged"}}
	components := map[string]bool{}
	paths := make([]string, 0, len(compiled.Evidence()))
	hasTestPath := false
	for _, item := range compiled.Evidence() {
		evidence[item.ID] = item
		paths = append(paths, item.Path)
		for _, component := range pathTerms(item.Path) {
			components[component] = true
		}
		if isTestPath(item.Path) {
			hasTestPath = true
		}
	}
	inferredScopes := policy.InferredScopes(paths)
	issues := map[string]bool{}
	for _, issue := range compiled.Issues() {
		issues[issue] = true
	}
	recentTypes := map[string]bool{}
	for _, kind := range compiled.RecentTypes() {
		recentTypes[kind] = true
	}
	results := make([]Result, len(candidates))
	for index, value := range candidates {
		results[index] = evaluate(value, evidence, components, hasTestPath, issues, recentTypes, inferredScopes, policy)
	}
	return results
}

func evaluate(value provider.Candidate, evidence map[string]repoctx.Evidence, components map[string]bool, hasTestPath bool, issues, recentTypes map[string]bool, inferredScopes []string, policy config.RepositoryPolicy) Result {
	result := Result{EvidenceIDs: append([]string(nil), value.EvidenceIDs...)}
	validEvidence := 0
	for _, id := range value.EvidenceIDs {
		_, found := evidence[id]
		result.Checks = append(result.Checks, Check{Name: "evidence_reference", Deterministic: true, Passed: found, Detail: evidenceDetail(id, found)})
		if found {
			validEvidence++
		} else {
			result.UnsupportedClaims = append(result.UnsupportedClaims, "unknown evidence reference")
		}
	}
	typeValid := conventionalTypes[value.Type] && policy.AllowsType(value.Type)
	result.Checks = append(result.Checks, Check{Name: "commit_type", Deterministic: true, Passed: typeValid, Detail: "built-in security floor plus repository type policy"})
	if !typeValid {
		result.UnsupportedClaims = append(result.UnsupportedClaims, "unsupported commit type")
	}
	subjectValid := len(value.Subject) > 0 && len(value.Subject) <= 120 && commitLength(value) <= policy.LineLength && !strings.ContainsAny(value.Subject, "\r\n")
	result.Checks = append(result.Checks, Check{Name: "subject_format", Deterministic: true, Passed: subjectValid, Detail: "non-empty single-line subject within effective repository line length"})
	if !subjectValid {
		result.UnsupportedClaims = append(result.UnsupportedClaims, "invalid subject")
	}

	componentMatch := subjectMatchesComponent(value.Subject, components)
	scopeValid := true
	if value.Scope != "" {
		scopeValid = scopeAllowed(value.Scope, components, inferredScopes, policy)
		result.Checks = append(result.Checks, Check{Name: "allowed_scope", Deterministic: true, Passed: scopeValid, Detail: "built-in scope relevance plus repository scope/path policy"})
		if !scopeValid {
			result.UnsupportedClaims = append(result.UnsupportedClaims, "unsupported scope")
		}
	}
	capitalized := capitalizationValid(value.Subject, policy.Capitalization)
	result.Checks = append(result.Checks, Check{Name: "capitalization", Deterministic: true, Passed: capitalized, Detail: "effective repository capitalization policy"})
	if !capitalized {
		result.UnsupportedClaims = append(result.UnsupportedClaims, "capitalization policy mismatch")
	}
	bodyValid := policy.Body != "required"
	result.Checks = append(result.Checks, Check{Name: "body_preference", Deterministic: true, Passed: bodyValid, Detail: "subject-only candidates cannot satisfy a required body"})
	if !bodyValid {
		result.UnsupportedClaims = append(result.UnsupportedClaims, "required commit body is unavailable")
	}
	for _, issue := range issuePattern.FindAllString(strings.ToUpper(value.Subject), -1) {
		found := issues[issue]
		result.Checks = append(result.Checks, Check{Name: "issue_reference", Deterministic: true, Passed: found, Detail: "issue must be inferred from the bounded branch identifier"})
		if !found {
			result.UnsupportedClaims = append(result.UnsupportedClaims, "unsupported issue identifier")
		}
	}
	subject := strings.ToLower(value.Subject)
	if testClaimPattern.MatchString(subject) {
		result.Checks = append(result.Checks, Check{Name: "claimed_tests", Deterministic: true, Passed: hasTestPath, Detail: "test claim requires a staged test-like path"})
		if !hasTestPath {
			result.UnsupportedClaims = append(result.UnsupportedClaims, "unsupported test claim")
		}
	}
	if value.Type == "fix" || fixClaimPattern.MatchString(subject) {
		passed := componentMatch
		result.Checks = append(result.Checks, Check{Name: "claimed_fix", Deterministic: false, Passed: passed, Detail: "path-component overlap is only a heuristic fix signal"})
		if !passed {
			result.UnsupportedClaims = append(result.UnsupportedClaims, "unsupported fix claim")
		}
	}
	if effectClaimPattern.MatchString(subject) {
		passed := componentMatch
		result.Checks = append(result.Checks, Check{Name: "behavioral_claim", Deterministic: false, Passed: passed, Detail: "behavioral consequences cannot be semantically proved from context"})
		if !passed {
			result.UnsupportedClaims = append(result.UnsupportedClaims, "unsupported behavioral claim")
		}
	}
	styleMatch := recentTypes[value.Type]
	result.Checks = append(result.Checks, Check{Name: "repository_style", Deterministic: false, Passed: styleMatch, Detail: "recent repository subject type is a ranking signal"})

	heuristics := 0
	for _, check := range result.Checks {
		if !check.Deterministic && (check.Name == "claimed_fix" || check.Name == "behavioral_claim") {
			heuristics++
		}
	}
	result.Score = validEvidence*30 + boolScore(typeValid, 20) + boolScore(subjectValid, 20) + boolScore(scopeValid, 10) + boolScore(capitalized, 10) + boolScore(bodyValid, 5) + boolScore(componentMatch, 10) + boolScore(styleMatch, 5) - len(result.UnsupportedClaims)*35
	switch {
	case len(result.UnsupportedClaims) > 0 || !typeValid || !subjectValid:
		result.State = Ungrounded
	case validEvidence == 0:
		result.State = InsufficientContext
	case heuristics > 0:
		result.State = PartiallyGrounded
	default:
		result.State = Grounded
	}
	return result
}

func Rank(results []Result, policy string) []int {
	indexes := make([]int, 0, len(results))
	for index, result := range results {
		if allowed(result.State, policy) {
			indexes = append(indexes, index)
		}
	}
	sort.SliceStable(indexes, func(left, right int) bool {
		first, second := results[indexes[left]], results[indexes[right]]
		if stateScore(first.State) == stateScore(second.State) {
			if first.Score == second.Score {
				return indexes[left] < indexes[right]
			}
			return first.Score > second.Score
		}
		return stateScore(first.State) > stateScore(second.State)
	})
	return indexes
}

func allowed(state State, policy string) bool {
	switch policy {
	case "quiet":
		return state == Grounded
	case "visible":
		return state != InsufficientContext
	case "conservative", "hintable":
		return state == Grounded || state == PartiallyGrounded
	default:
		return false
	}
}

func stateScore(state State) int {
	switch state {
	case Grounded:
		return 4
	case PartiallyGrounded:
		return 3
	case Ungrounded:
		return 2
	default:
		return 1
	}
}

func evidenceDetail(id string, found bool) string {
	if found {
		return "reference exists in bounded staged context: " + id
	}
	return "reference is absent from bounded staged context: " + id
}

func boolScore(value bool, score int) int {
	if value {
		return score
	}
	return 0
}

func isTestPath(value string) bool {
	lower := strings.ToLower(filepath.ToSlash(value))
	base := filepath.Base(lower)
	return strings.Contains(base, "test") || strings.Contains(base, "spec") || strings.Contains(lower, "/testdata/") || strings.Contains(lower, "/tests/")
}

func pathTerms(value string) []string {
	value = strings.ToLower(filepath.ToSlash(value))
	value = strings.TrimSuffix(value, filepath.Ext(value))
	terms := wordPattern.FindAllString(value, -1)
	result := make([]string, 0, len(terms))
	for _, term := range terms {
		if term != "src" && term != "internal" && term != "cmd" {
			result = append(result, term)
		}
	}
	return result
}

func subjectMatchesComponent(subject string, components map[string]bool) bool {
	for _, word := range wordPattern.FindAllString(strings.ToLower(subject), -1) {
		if components[word] {
			return true
		}
	}
	return false
}

func scopeMatchesComponent(scope string, components map[string]bool) bool {
	if scope == "repo" {
		return true
	}
	for _, word := range wordPattern.FindAllString(strings.ToLower(scope), -1) {
		if components[word] {
			return true
		}
	}
	return false
}

func scopeAllowed(scope string, components map[string]bool, inferred []string, policy config.RepositoryPolicy) bool {
	if len(policy.Scopes) > 0 && !stringContains(policy.Scopes, scope) {
		return false
	}
	if len(inferred) > 0 {
		return stringContains(inferred, scope)
	}
	return scopeMatchesComponent(scope, components)
}

func stringContains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func commitLength(value provider.Candidate) int {
	length := len(value.Type) + 2 + len(value.Subject)
	if value.Scope != "" {
		length += len(value.Scope) + 2
	}
	return length
}

func capitalizationValid(subject, policy string) bool {
	for _, character := range subject {
		if !unicode.IsLetter(character) {
			continue
		}
		if policy == "sentence" {
			return unicode.IsUpper(character)
		}
		return unicode.IsLower(character)
	}
	return false
}
