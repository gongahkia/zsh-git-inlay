package provider

import (
	"encoding/json"
	"testing"
)

func FuzzStructuredResponse(f *testing.F) {
	f.Add(`{"candidates":[{"type":"fix","scope":"cache","subject":"update cache records","evidence_ids":["change:0"]}]}`)
	f.Add(`{"candidates":[{"type":"FIX","subject":"bad","evidence_ids":["change:0"]}]}`)
	f.Add(`{"candidates":[]}`)
	f.Fuzz(func(t *testing.T, content string) {
		if len(content) > MaxResponseBytes {
			return
		}
		var response struct {
			Candidates []Candidate `json:"candidates"`
		}
		if json.Unmarshal([]byte(content), &response) != nil {
			return
		}
		_, _ = (Response{Candidates: response.Candidates, Metadata: Deterministic{}.Metadata()}).ToCandidates()
	})
}
