package command

import (
	"strings"
	"testing"
)

func FuzzParseAndSuggestion(f *testing.F) {
	for _, seed := range []string{
		"git commit -m ", "git commit -m 'fix", "git commit -m $(touch nope)", "git commit -m ;", "command git commit --message ",
	} {
		f.Add(seed, len(seed), 0)
	}
	f.Fuzz(func(t *testing.T, buffer string, cursor, cycle int) {
		if len(buffer) > 4096 {
			return
		}
		cursor = int(uint(cursor) % uint(len(buffer)+1))
		_, _ = Parse(buffer, cursor)
		suggestion, ok := Suggestion(buffer, cursor, testCandidates, cycle)
		if ok && !strings.HasPrefix(suggestion, buffer) {
			t.Fatalf("suggestion %q does not extend buffer %q", suggestion, buffer)
		}
	})
}
