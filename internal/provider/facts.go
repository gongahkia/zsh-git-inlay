package provider

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/gongahkia/zsh-git-inlay/internal/candidate"
	"github.com/gongahkia/zsh-git-inlay/internal/repoctx"
)

var declaration = regexp.MustCompile(`^(?:func\s+(?:\([^)]*\)\s+)?|(?:export\s+)?(?:async\s+)?(?:function|class|def)\s+|(?:pub\s+)?(?:async\s+)?fn\s+)([A-Za-z_][A-Za-z0-9_]*)`)
var dependency = regexp.MustCompile(`^\s*"([a-zA-Z0-9._/-]+)"\s*:\s*"([0-9][a-zA-Z0-9._-]*)"\s*,?\s*$`)
var goDependency = regexp.MustCompile(`^\s*(?:require\s+)?([a-zA-Z0-9._/-]+)\s+(v[0-9][a-zA-Z0-9._-]*)\s*$`)

// factualCandidates reads only compiler-redacted staged evidence. The original
// metadata generator remains unchanged as a reproducible comparison baseline.
func factualCandidates(evidence []repoctx.Evidence, patch string, baseline []Candidate) []Candidate {
	if len(evidence) == 0 {
		return baseline
	}
	var result []Candidate
	add := func(value Candidate) {
		if value.Scope != "" && len(value.Scope) > 40 {
			value.Scope = ""
		}
		if _, err := (Response{Candidates: []Candidate{value}}).ToCandidates(); err != nil {
			return
		}
		for _, existing := range result {
			if existing.Type == value.Type && existing.Scope == value.Scope && existing.Subject == value.Subject {
				return
			}
		}
		result = append(result, value)
	}
	// Specific semantic facts precede file summaries; all are checked again by
	// repository policy and grounding before publication.
	for _, fact := range patchFacts(patch, evidence) {
		if fact.Scope == "" {
			for _, item := range evidence {
				if len(fact.EvidenceIDs) > 0 && fact.EvidenceIDs[0] == item.ID {
					fact.Scope = factScope(item.Path)
					break
				}
			}
		}
		add(fact)
	}
	if len(evidence) > 1 {
		value := baseline[0]
		names := []string{}
		for _, item := range evidence {
			if len(names) < 2 {
				names = append(names, path.Base(item.Path))
			}
		}
		subject := value.Subject + " including " + strings.Join(names, " and ")
		if len(subject)+len(value.Scope)+len(value.Type)+5 <= 120 {
			value.Subject = subject
		}
		value.EvidenceIDs = []string{"metadata:staged"}
		add(value)
	}
	for _, item := range evidence {
		name := path.Base(item.Path)
		if len(name) > 60 {
			continue
		}
		verb := "update"
		switch {
		case item.Status == "A":
			verb = "add"
		case item.Status == "D":
			verb = "remove"
		case strings.HasPrefix(item.Status, "R"):
			verb = "rename"
		case strings.HasPrefix(item.Status, "C"):
			verb = "copy"
		}
		kind := baseline[0].Type
		if strings.HasSuffix(item.Path, ".md") || strings.HasPrefix(item.Path, "docs/") {
			kind = "docs"
		}
		add(Candidate{Type: kind, Scope: factScope(item.Path), Subject: verb + " " + name, EvidenceIDs: []string{item.ID}})
	}
	// One overall summary is useful; padding with generic synonyms is not.
	add(baseline[0])
	if len(result) > candidate.MaxCandidates {
		result = result[:candidate.MaxCandidates]
	}
	return result
}

func patchFacts(patch string, evidence []repoctx.Evidence) []Candidate {
	// Parse only literal unified-diff file sections. Quoted/ambiguous headers are
	// ignored; filenames remain untrusted and pass shell validation later.
	byPath := map[string]repoctx.Evidence{}
	for _, item := range evidence {
		byPath[item.Path] = item
	}
	var result []Candidate
	current := ""
	dependencySection := false
	added, removed := map[string]string{}, map[string]string{}
	addedDecl, removedDecl := map[string]bool{}, map[string]bool{}
	flush := func() {
		item, ok := byPath[current]
		if !ok {
			return
		}
		names := make([]string, 0, len(added))
		for name := range added {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if old, ok := removed[name]; ok && old != added[name] {
				result = append(result, Candidate{Type: "build", Subject: fmt.Sprintf("update %s from %s to %s in %s", name, old, added[name], path.Base(item.Path)), EvidenceIDs: []string{item.ID}})
			}
		}
		names = nil
		for name := range addedDecl {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !removedDecl[name] {
				result = append(result, Candidate{Type: "chore", Subject: "add " + name + " declaration in " + path.Base(item.Path), EvidenceIDs: []string{item.ID}})
			}
		}
	}
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			current = ""
			dependencySection = false
			added, removed = map[string]string{}, map[string]string{}
			addedDecl, removedDecl = map[string]bool{}, map[string]bool{}
		}
		if strings.HasPrefix(line, "@@ ") {
			dependencySection = false
		}
		if strings.HasPrefix(line, "+++ b/") {
			current = strings.TrimPrefix(line, "+++ b/")
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") || len(line) < 2 {
			continue
		}
		if len(line) > 1 && (line[0] == ' ' || line[0] == '+' || line[0] == '-') && strings.Contains(line, `": {`) {
			text := strings.TrimSpace(line[1:])
			dependencySection = strings.HasPrefix(text, `"dependencies":`) || strings.HasPrefix(text, `"devDependencies":`) || strings.HasPrefix(text, `"peerDependencies":`) || strings.HasPrefix(text, `"optionalDependencies":`)
		}
		if len(line) > 1 && strings.HasPrefix(strings.TrimSpace(line[1:]), "}") {
			dependencySection = false
		}
		if line[0] != '+' && line[0] != '-' {
			continue
		}
		text := strings.TrimSpace(line[1:])
		if match := declaration.FindStringSubmatch(text); match != nil {
			if line[0] == '+' {
				addedDecl[match[1]] = true
			} else {
				removedDecl[match[1]] = true
			}
		}
		base := path.Base(current)
		if (base != "package.json" && base != "go.mod") || (base == "package.json" && !dependencySection) {
			continue
		}
		pattern := dependency
		if base == "go.mod" {
			pattern = goDependency
		}
		if match := pattern.FindStringSubmatch(text); match != nil {
			if line[0] == '+' {
				added[match[1]] = match[2]
			} else {
				removed[match[1]] = match[2]
			}
		}
	}
	flush()
	return result
}

func factScope(file string) string {
	first, _, _ := strings.Cut(strings.TrimPrefix(file, "./"), "/")
	first = strings.Trim(strings.ToLower(first), "-.")
	if messageScope.MatchString(first) {
		return first
	}
	return ""
}
