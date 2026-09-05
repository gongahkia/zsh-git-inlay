package command

import "testing"

func BenchmarkSuggestion(b *testing.B) {
	buffer := "git commit -m "
	for index := 0; index < b.N; index++ {
		if _, ok := Suggestion(buffer, len(buffer), testCandidates, index); !ok {
			b.Fatal("candidate was not suggested")
		}
	}
}
