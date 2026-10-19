package evaluation

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/gongahkia/zsh-git-inlay/internal/candidate"
)

type ReviewCase struct {
	ID           string                `json:"id"`
	SourceCommit string                `json:"source_commit,omitempty"`
	Context      []Change              `json:"public_fixture_context,omitempty"`
	A            []candidate.Candidate `json:"a"`
	B            []candidate.Candidate `json:"b"`
}

type ReviewPacket struct {
	Version      string       `json:"version"`
	Instructions string       `json:"instructions"`
	Cases        []ReviewCase `json:"cases"`
}

type ReviewKey struct {
	Version      string           `json:"version"`
	PacketSHA256 string           `json:"packet_sha256"`
	CorpusDigest string           `json:"corpus_digest"`
	Left         ProviderMetadata `json:"left"`
	Right        ProviderMetadata `json:"right"`
	AIsLeft      map[string]bool  `json:"a_is_left"`
}

type HumanScore struct {
	FactualAdequacy     int     `json:"factual_adequacy"`
	Specificity         int     `json:"specificity"`
	EditCharacters      int     `json:"edit_characters"`
	TimeToUsableSeconds float64 `json:"time_to_usable_seconds"`
}

type HumanRating struct {
	ID        string     `json:"id"`
	Preferred string     `json:"preferred"`
	A         HumanScore `json:"a"`
	B         HumanScore `json:"b"`
}

type HumanOutcome struct {
	Provider                   ProviderMetadata `json:"provider"`
	Preferred                  int              `json:"preferred"`
	AverageFactualAdequacy     float64          `json:"average_factual_adequacy"`
	AverageSpecificity         float64          `json:"average_specificity"`
	AverageEditCharacters      float64          `json:"average_edit_characters"`
	AverageTimeToUsableSeconds float64          `json:"average_time_to_usable_seconds"`
}

type HumanSummary struct {
	Evidence  string       `json:"evidence"`
	Completed int          `json:"completed"`
	Total     int          `json:"total"`
	Complete  bool         `json:"complete"`
	Ties      int          `json:"ties"`
	Neither   int          `json:"neither"`
	Left      HumanOutcome `json:"left"`
	Right     HumanOutcome `json:"right"`
}

// PrepareReview randomizes labels independently per case. The private key is a
// separate administrator artifact and must not be sent to a blinded reviewer.
func PrepareReview(left, right Report) (ReviewPacket, ReviewKey, error) {
	packet := ReviewPacket{Version: "v1", Instructions: "Review A and B against the public fixture context, or inspect the source commit diff locally for history replays. Choose the best candidate on each side. Score factual adequacy and specificity 1–5, record characters edited and seconds to a usable message, and prefer A, B, tie or neither. Ratings are explicit human evidence; do not infer acceptance or factual correctness from automatic checks. Keep key.json separate until scoring is finished."}
	key := ReviewKey{Version: "v1", CorpusDigest: left.CorpusDigest, Left: left.Provider, Right: right.Provider, AIsLeft: map[string]bool{}}
	if left.CorpusDigest == "" || left.CorpusDigest != right.CorpusDigest || left.Source.Kind != right.Source.Kind || left.Source.Partition != right.Source.Partition || len(left.Cases) == 0 || len(left.Cases) != len(right.Cases) {
		return packet, key, fmt.Errorf("comparison requires identical nonempty corpus, source and partition")
	}
	byID := map[string]Result{}
	for _, value := range right.Cases {
		if _, ok := byID[value.ID]; ok {
			return packet, key, fmt.Errorf("duplicate comparison case")
		}
		byID[value.ID] = value
	}
	contextByID := map[string][]Change{}
	if left.Source.Kind == "synthetic_fixture" {
		if corpus, err := LoadCorpusVersion(left.CorpusVersion); err == nil && corpus.Digest == left.CorpusDigest {
			for _, fixture := range corpus.Cases {
				contextByID[fixture.ID] = fixture.Changes
			}
		}
	}
	for _, value := range left.Cases {
		other, ok := byID[value.ID]
		if !ok || value.ID == "" || value.SourceCommit != other.SourceCommit {
			return packet, key, fmt.Errorf("comparison cases must match exactly")
		}
		if _, duplicate := key.AIsLeft[value.ID]; duplicate {
			return packet, key, fmt.Errorf("duplicate comparison case")
		}
		for _, values := range [][]candidate.Candidate{value.Candidates, other.Candidates} {
			if len(values) == 0 || len(values) > candidate.MaxCandidates {
				return packet, key, fmt.Errorf("case %s lacks prepared candidates; regenerate a current private report", value.ID)
			}
			for _, item := range values {
				if !candidate.Valid(item) {
					return packet, key, fmt.Errorf("invalid review candidate")
				}
			}
		}
		var choice [1]byte
		if _, err := rand.Read(choice[:]); err != nil {
			return packet, key, err
		}
		aIsLeft := choice[0]&1 == 0
		item := ReviewCase{ID: value.ID, SourceCommit: value.SourceCommit, Context: contextByID[value.ID], A: value.Candidates, B: other.Candidates}
		if !aIsLeft {
			item.A, item.B = item.B, item.A
		}
		key.AIsLeft[value.ID] = aIsLeft
		packet.Cases = append(packet.Cases, item)
	}
	encoded, _ := json.Marshal(packet)
	digest := sha256.Sum256(encoded)
	key.PacketSHA256 = hex.EncodeToString(digest[:])
	return packet, key, nil
}

func WriteReview(directory string, packet ReviewPacket, key ReviewKey) error {
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	if len(packet.Cases) == 0 {
		return fmt.Errorf("empty review packet")
	}
	template := make([]HumanRating, len(packet.Cases))
	for index, item := range packet.Cases {
		template[index].ID = item.ID
	}
	outputs := map[string]any{"packet.json": packet, "key.json": key, "ratings.example.json": template}
	for name := range outputs {
		if _, err := os.Lstat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			return fmt.Errorf("review output already exists or is inaccessible: %s", name)
		}
	}
	for name, value := range outputs {
		encoded, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		if err := writePrivate(filepath.Join(directory, name), append(encoded, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func SummarizeReview(packet ReviewPacket, key ReviewKey, ratings []HumanRating) (HumanSummary, error) {
	summary := HumanSummary{Evidence: "explicit_human_review; manual measurements, separate from inferred learning signals", Total: len(packet.Cases), Left: HumanOutcome{Provider: key.Left}, Right: HumanOutcome{Provider: key.Right}}
	encoded, _ := json.Marshal(packet)
	digest := sha256.Sum256(encoded)
	if packet.Version != "v1" || key.Version != "v1" || hex.EncodeToString(digest[:]) != key.PacketSHA256 || len(key.AIsLeft) != len(packet.Cases) {
		return summary, fmt.Errorf("review packet/key mismatch")
	}
	cases := map[string]bool{}
	for _, item := range packet.Cases {
		if cases[item.ID] {
			return summary, fmt.Errorf("duplicate packet case")
		}
		cases[item.ID] = true
		if _, ok := key.AIsLeft[item.ID]; !ok {
			return summary, fmt.Errorf("missing key mapping")
		}
	}
	seen := map[string]bool{}
	for _, rating := range ratings {
		if !cases[rating.ID] || seen[rating.ID] {
			return summary, fmt.Errorf("unknown or duplicate rating %q", rating.ID)
		}
		seen[rating.ID] = true
		for _, score := range []HumanScore{rating.A, rating.B} {
			if score.FactualAdequacy < 1 || score.FactualAdequacy > 5 || score.Specificity < 1 || score.Specificity > 5 || score.EditCharacters < 0 || score.TimeToUsableSeconds <= 0 || score.TimeToUsableSeconds > 86400 || math.IsNaN(score.TimeToUsableSeconds) || math.IsInf(score.TimeToUsableSeconds, 0) {
				return summary, fmt.Errorf("invalid human score for %s", rating.ID)
			}
		}
		a, b := &summary.Left, &summary.Right
		if !key.AIsLeft[rating.ID] {
			a, b = b, a
		}
		switch rating.Preferred {
		case "A":
			a.Preferred++
		case "B":
			b.Preferred++
		case "tie":
			summary.Ties++
		case "neither":
			summary.Neither++
		default:
			return summary, fmt.Errorf("preferred must be A, B, tie or neither")
		}
		for _, pair := range []struct {
			outcome *HumanOutcome
			score   HumanScore
		}{{a, rating.A}, {b, rating.B}} {
			pair.outcome.AverageFactualAdequacy += float64(pair.score.FactualAdequacy)
			pair.outcome.AverageSpecificity += float64(pair.score.Specificity)
			pair.outcome.AverageEditCharacters += float64(pair.score.EditCharacters)
			pair.outcome.AverageTimeToUsableSeconds += pair.score.TimeToUsableSeconds
		}
		summary.Completed++
	}
	if summary.Completed == 0 {
		return summary, fmt.Errorf("at least one explicit human rating is required")
	}
	for _, value := range []*HumanOutcome{&summary.Left, &summary.Right} {
		n := float64(summary.Completed)
		value.AverageFactualAdequacy /= n
		value.AverageSpecificity /= n
		value.AverageEditCharacters /= n
		value.AverageTimeToUsableSeconds /= n
	}
	summary.Complete = summary.Completed == summary.Total
	return summary, nil
}
