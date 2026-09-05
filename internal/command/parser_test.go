package command

import (
	"testing"

	"github.com/gongahkia/zsh-git-inlay/internal/candidate"
)

var testCandidates = []candidate.Candidate{{Message: "fix(cache): invalidate stale entries", Rank: 0}, {Message: "chore(cache): refresh staged changes", Rank: 1}, {Message: "test(cache): update test coverage", Rank: 2}}

func TestParseSupportedForms(t *testing.T) {
	for _, input := range []string{
		"git commit -m ", "git commit --message ", "git commit -am ", "git commit -a -m ", "command git commit -m ", "git   commit --signoff -m ",
	} {
		t.Run(input, func(t *testing.T) {
			if _, ok := Parse(input, len(input)); !ok {
				t.Fatal("not parsed")
			}
		})
	}
}

func TestSuggestionQuotesAndConstrainedPrefixes(t *testing.T) {
	tests := []struct{ input, want string }{
		{"git commit -m ", "git commit -m 'fix(cache): invalidate stale entries'"},
		{"git commit -m 'fix", "git commit -m 'fix(cache): invalidate stale entries'"},
		{"git commit -m \"fix", "git commit -m \"fix(cache): invalidate stale entries\""},
		{"git commit -m fix", "git commit -m fix\\(cache\\)\\:\\ invalidate\\ stale\\ entries"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, ok := Suggestion(test.input, len(test.input), testCandidates, 0)
			if !ok || got != test.want {
				t.Fatalf("got %q (ok=%v), want %q", got, ok, test.want)
			}
		})
	}
	if got, ok := Suggestion("git commit -m nope", len("git commit -m nope"), testCandidates, 0); ok || got != "" {
		t.Fatalf("unmatched prefix suggested %q", got)
	}
	matching := []candidate.Candidate{
		{Message: "chore(repo): update staged files", Rank: 0},
		{Message: "fix(cache): invalidate stale entries", Rank: 1},
		{Message: "fix(cache): refresh stale metadata", Rank: 2},
	}
	if got, ok := Suggestion("git commit -m 'fix(cache): ", len("git commit -m 'fix(cache): "), matching, 0); !ok || got != "git commit -m 'fix(cache): invalidate stale entries'" {
		t.Fatalf("prefix rerank selected %q (ok=%v)", got, ok)
	}
	if got, ok := Suggestion("git commit -m 'fix(cache): ", len("git commit -m 'fix(cache): "), matching, 1); !ok || got != "git commit -m 'fix(cache): refresh stale metadata'" {
		t.Fatalf("prefix rerank cycle selected %q (ok=%v)", got, ok)
	}
	if got, ok := Suggestion("git commit -m ", len("git commit -m "), testCandidates, 1); !ok || got == "" || got == "git commit -m 'fix(cache): invalidate stale entries'" {
		t.Fatalf("cycle did not select another candidate: %q", got)
	}
}

func TestParseRejectsUnsupportedAndUnsafeInput(t *testing.T) {
	for _, input := range []string{
		"git commit --amend -m ", "git commit --fixup HEAD -m ", "git commit --squash HEAD -m ", "git merge -m ", "git tag -m ", "echo git commit -m ", "git commit -m 'closed'", "git commit -m $(touch nope)", "git commit -m ;",
	} {
		t.Run(input, func(t *testing.T) {
			if _, ok := Parse(input, len(input)); ok {
				t.Fatal("unexpected parse")
			}
		})
	}
	if _, ok := Parse("git commit -m ", 2); ok {
		t.Fatal("cursor before end accepted")
	}
}
