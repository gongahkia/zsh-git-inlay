package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/zsh-git-inlay/internal/candidate"
)

func TestBlindedReviewUnblindsOnlyExplicitValidatedRatings(t *testing.T) {
	left := Report{CorpusDigest: strings.Repeat("a", 64), Source: Source{Kind: "synthetic_fixture", Partition: HeldOutSet}, Provider: ProviderMetadata{Name: "left-model"}, Cases: []Result{{ID: "one", Candidates: []candidate.Candidate{{Message: "chore: update files", Rank: 0}}}}}
	right := left
	right.Provider.Name = "right-model"
	right.Cases = []Result{{ID: "one", Candidates: []candidate.Candidate{{Message: "chore: add cache.go", Rank: 0}}}}
	packet, key, err := PrepareReview(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(packet.Instructions, "left-model") || strings.Contains(packet.Instructions, "right-model") {
		t.Fatal("unblinded packet")
	}
	score := HumanScore{FactualAdequacy: 4, Specificity: 3, EditCharacters: 2, TimeToUsableSeconds: 6}
	ratings := []HumanRating{{ID: "one", Preferred: "A", A: score, B: score}}
	summary, err := SummarizeReview(packet, key, ratings)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Complete || summary.Completed != 1 || summary.Left.AverageTimeToUsableSeconds != 6 {
		t.Fatalf("summary=%#v", summary)
	}
	if (summary.Left.Preferred == 1) != key.AIsLeft["one"] {
		t.Fatal("incorrect unblinding")
	}
	if _, err := SummarizeReview(packet, key, append(ratings, ratings[0])); err == nil {
		t.Fatal("duplicate rating accepted")
	}
	if _, err := SummarizeReview(packet, key, []HumanRating{{ID: "one", Preferred: "tie"}}); err == nil {
		t.Fatal("unfilled template accepted")
	}
	directory := filepath.Join(t.TempDir(), "review")
	if err := WriteReview(directory, packet, key); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"packet.json", "key.json", "ratings.example.json"} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private %s: %v %v", name, info, err)
		}
	}
	if err := WriteReview(directory, packet, key); err == nil {
		t.Fatal("review evidence overwritten")
	}
	packet.Cases[0].A[0].Message = "chore: tampered"
	if _, err := SummarizeReview(packet, key, ratings); err == nil {
		t.Fatal("tampered packet accepted")
	}
}

func TestFrozenCorpusRemainsAvailable(t *testing.T) {
	corpus, err := LoadCorpusVersion("v2")
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 15 || corpus.Digest != "99cbe770cb27d2e9e1a99f74e8d16fb6cb5b88e50f5c3cfd8a417d6287fd6361" {
		t.Fatalf("frozen corpus changed: %#v", corpus)
	}
}
