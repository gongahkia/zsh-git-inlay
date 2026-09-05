package cloud

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGrantsArePrivateScopedAndReplaceWholeCapabilitySet(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := store.Grant("openai")
	if err != nil || len(grant.Classes) != 0 {
		t.Fatalf("default grant=%#v err=%v", grant, err)
	}
	first, err := store.Set("openai", []ContextClass{History, StagedDiff})
	if err != nil || !SameClasses(first.Classes, []ContextClass{StagedDiff, History}) {
		t.Fatalf("first grant=%#v err=%v", first, err)
	}
	second, err := store.Set("openai", []ContextClass{Activity})
	if err != nil || !SameClasses(second.Classes, []ContextClass{Activity}) {
		t.Fatalf("replacement grant=%#v err=%v", second, err)
	}
	info, err := os.Stat(store.Path())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("grant mode=%v err=%v", info, err)
	}
	content, err := os.ReadFile(store.Path())
	if err != nil || string(content) == "" || string(content) == "[]" || string(content) == "{}" {
		t.Fatalf("grant content=%q err=%v", content, err)
	}
	if err := store.Revoke("openai"); err != nil {
		t.Fatal(err)
	}
	grant, err = store.Grant("openai")
	if err != nil || len(grant.Classes) != 0 {
		t.Fatalf("revoked grant=%#v err=%v", grant, err)
	}
}

func TestGrantsFailClosedForInvalidOrNonPrivateRecords(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, classes := range [][]ContextClass{{}, {StagedDiff, StagedDiff}, {"unknown"}} {
		if _, err := store.Set("openai", classes); err == nil {
			t.Fatalf("invalid classes accepted: %#v", classes)
		}
	}
	if _, err := store.Set("other", []ContextClass{StagedDiff}); err == nil {
		t.Fatal("unknown provider accepted")
	}
	if err := os.WriteFile(filepath.Join(directory, "cloud-grants.json"), []byte(`{"schema_version":1,"grants":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Status(); err == nil {
		t.Fatal("non-private record accepted")
	}
}
