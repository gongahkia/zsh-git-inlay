package feedback

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFeedbackIsBoundedPrivateAndKeepsOnlyLatestExplicitLabel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "feedback.json")
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	state := State{Schema: 1}
	for index := 0; index < MaxRecords; index++ {
		state.Records = append(state.Records, Record{Repository: strings.Repeat("a", 64), Worktree: strings.Repeat("b", 64), Fingerprint: Hash(fmt.Sprint(index)), MessageHash: Hash("private candidate"), Label: "too-generic", Created: time.Now().UTC()})
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	record := Record{Repository: strings.Repeat("a", 64), Worktree: strings.Repeat("b", 64), Fingerprint: Hash("new state"), MessageHash: Hash("private candidate"), Label: "incorrect"}
	if err := Add(path, record); err != nil {
		t.Fatal(err)
	}
	record.Label = "useful"
	if err := Add(path, record); err != nil {
		t.Fatal(err)
	}
	stored, err := Load(path)
	if err != nil || len(stored.Records) != MaxRecords || stored.Records[MaxRecords-1].Label != "useful" || stored.Records[0].Fingerprint == state.Records[0].Fingerprint {
		t.Fatalf("incorrect retention/deduplication: %d %v", len(stored.Records), err)
	}
	data, _ = os.ReadFile(path)
	if bytes.Contains(data, []byte("private candidate")) || bytes.Contains(data, []byte("accepted")) {
		t.Fatal("feedback persisted a message or inferred acceptance")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("feedback is not private: %v", err)
	}
	if err := Clear(path, record.Repository); err != nil {
		t.Fatal(err)
	}
	stored, err = Load(path)
	if err != nil || len(stored.Records) != 0 {
		t.Fatalf("feedback not cleared: %v", err)
	}
	files, _ := os.ReadDir(filepath.Dir(path))
	if len(files) != 2 {
		t.Fatalf("feedback clear retained hidden backup history: %v", files)
	}
}

func TestFeedbackRejectsUnrecognizedLabelsAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "feedback.json")
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Add(path, Record{Label: "accepted"}); err == nil {
		t.Fatal("inferred acceptance treated as explicit feedback")
	}
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Clear(path, strings.Repeat("a", 64)); err == nil {
		t.Fatal("corrupt feedback silently overwritten")
	}
}
