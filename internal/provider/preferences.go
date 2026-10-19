package provider

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/repoctx"
)

// AdaptPreferences changes presentation before grounding. It never changes the
// proposed commit type or invents evidence to satisfy a user's allowlist.
func AdaptPreferences(response Response, evidence []repoctx.Evidence, policy config.RepositoryPolicy) Response {
	adapted := response
	adapted.Candidates = make([]Candidate, 0, len(response.Candidates))
	for _, original := range response.Candidates {
		value := original
		paths := []string{}
		matching := []repoctx.Evidence{}
		for _, item := range evidence {
			for _, id := range value.EvidenceIDs {
				if id == item.ID || id == "metadata:staged" {
					paths = append(paths, item.Path)
					matching = append(matching, item)
					break
				}
			}
		}
		if policy.ScopeMode == "none" {
			value.Scope = ""
		} else if inferred := policy.InferredScopes(paths); len(inferred) == 1 {
			value.Scope = inferred[0]
		}
		// Shorten a long proposal by selecting a literal staged-file fact,
		// rather than clipping a sentence and potentially reversing its meaning.
		if messageLength(value) > policy.LineLength || len(value.Subject) > 120 {
			for _, item := range matching {
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
				short := value
				short.Subject = verb + " " + path.Base(item.Path)
				if len(short.Subject) <= 120 && messageLength(short) <= policy.LineLength {
					value = short
					break
				}
			}
		}
		value.Subject = capitalize(value.Subject, policy.Capitalization)
		duplicate := false
		for _, existing := range adapted.Candidates {
			duplicate = duplicate || (value.Type == existing.Type && value.Scope == existing.Scope && value.Subject == existing.Subject)
		}
		if !duplicate {
			adapted.Candidates = append(adapted.Candidates, value)
		}
	}
	return adapted
}

func messageLength(value Candidate) int {
	length := len(value.Type) + 2 + len(value.Subject)
	if value.Scope != "" {
		length += len(value.Scope) + 2
	}
	return length
}

func capitalize(subject, policy string) string {
	for offset, character := range subject {
		if !unicode.IsLetter(character) {
			continue
		}
		replacement := unicode.ToLower(character)
		if policy == "sentence" {
			replacement = unicode.ToUpper(character)
		}
		return subject[:offset] + string(replacement) + subject[offset+utf8.RuneLen(character):]
	}
	return subject
}
