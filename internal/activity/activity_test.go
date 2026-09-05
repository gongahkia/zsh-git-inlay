package activity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	repositoryA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	worktreeA   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	repositoryB = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	worktreeB   = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
)

func TestActivityRequiresConsentAndRevocationClearsMemory(t *testing.T) {
	permissions := Permissions{}
	store := New(DefaultSettings(), func() (Permissions, error) { return permissions, nil })
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	if decision := store.Ingest(event(repositoryA, worktreeA, "git.index_changed", now)); decision.Accepted || decision.Reason != "activity permission is disabled" {
		t.Fatalf("pre-consent decision = %#v", decision)
	}
	permissions.Activity = true
	if inspection := store.Inspect(repositoryA, worktreeA); len(inspection.Selected) != 0 || len(inspection.Excluded) != 0 {
		t.Fatalf("pre-consent event was retained: %#v", inspection)
	}
	if decision := store.Ingest(event(repositoryA, worktreeA, "git.index_changed", now)); !decision.Accepted {
		t.Fatalf("consented event = %#v", decision)
	}
	if provenance := store.Provenance(repositoryA, worktreeA); len(provenance.Signals) != 1 || provenance.Fingerprint == "" {
		t.Fatalf("activity provenance = %#v", provenance)
	}
	permissions.Activity = false
	if provenance := store.Provenance(repositoryA, worktreeA); provenance.Fingerprint != "" || len(provenance.Signals) != 0 {
		t.Fatalf("revoked activity still affected signals: %#v", provenance)
	}
	permissions.Activity = true
	if inspection := store.Inspect(repositoryA, worktreeA); len(inspection.Selected) != 0 {
		t.Fatalf("revoked events survived: %#v", inspection)
	}
}

func TestActivityReplayRedactionTTLAndRepositoryIsolation(t *testing.T) {
	permissions := Permissions{Activity: true}
	store := New(Settings{Retention: time.Minute, MaxEvents: 4, MaxScopes: 2}, func() (Permissions, error) { return permissions, nil })
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	content, err := os.ReadFile(filepath.Join("testdata", "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture []Event
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatal(err)
	}
	for index := range fixture {
		fixture[index].Timestamp = now.Add(time.Duration(index) * time.Second)
		if decision := store.Ingest(fixture[index]); !decision.Accepted {
			t.Fatalf("fixture event %d = %#v", index, decision)
		}
	}
	private := event(repositoryA, worktreeA, "shell.command_finished", now)
	private.Data = map[string]string{"token": "ghp_not_retained", "note": "password=not_retained"}
	if decision := store.Ingest(private); !decision.Accepted {
		t.Fatalf("private event = %#v", decision)
	}
	secret := event(repositoryA, worktreeA, "build.completed", now)
	secret.Sensitivity = Secret
	secret.Data = map[string]string{"detail": "secret output"}
	if decision := store.Ingest(secret); !decision.Accepted {
		t.Fatalf("secret event = %#v", decision)
	}
	if decision := store.Ingest(event(repositoryB, worktreeB, "editor.file_saved", now)); !decision.Accepted {
		t.Fatalf("other repository event = %#v", decision)
	}
	inspection := store.Inspect(repositoryA, worktreeA)
	if len(inspection.Selected) != 4 || inspection.Selected[2].Data["token"] != "[REDACTED]" || inspection.Selected[2].Data["note"] != "[REDACTED]" || inspection.Selected[3].Data != nil {
		t.Fatalf("redacted inspection = %#v", inspection)
	}
	if signals := store.Provenance(repositoryB, worktreeB).Signals; len(signals) != 1 || signals[0].Kind != "editor.file_saved" {
		t.Fatalf("cross-repository signals = %#v", signals)
	}
	if signals := store.Provenance(repositoryA, worktreeA).Signals; len(signals) != 4 {
		t.Fatalf("repository A signals = %#v", signals)
	}
	now = now.Add(2 * time.Minute)
	if provenance := store.Provenance(repositoryA, worktreeA); provenance.Fingerprint != "" {
		t.Fatalf("expired events affected activity: %#v", provenance)
	}
}

func TestActivityRejectsMalformedProducerWithoutAffectingLaterEvents(t *testing.T) {
	store := New(DefaultSettings(), func() (Permissions, error) { return Permissions{Activity: true}, nil })
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	invalid := event(repositoryA, worktreeA, "unknown", now)
	if decision := store.Ingest(invalid); decision.Accepted || decision.Reason != "invalid event" {
		t.Fatalf("malformed event = %#v", decision)
	}
	if decision := store.Ingest(event(repositoryA, worktreeA, "git.head_changed", now)); !decision.Accepted {
		t.Fatalf("valid event after malformed event = %#v", decision)
	}
	inspection := store.Inspect(repositoryA, worktreeA)
	if len(inspection.Selected) != 1 || inspection.Excluded["invalid event"] != 1 {
		t.Fatalf("producer isolation inspection = %#v", inspection)
	}
}

func TestActivityRejectsExpiredEventsAndBoundsScopes(t *testing.T) {
	store := New(Settings{Retention: time.Minute, MaxEvents: 1, MaxScopes: 1}, func() (Permissions, error) { return Permissions{Activity: true}, nil })
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	if decision := store.Ingest(event(repositoryA, worktreeA, "test.completed", now.Add(-2*time.Minute))); decision.Accepted || decision.Reason != "expired event" {
		t.Fatalf("expired event = %#v", decision)
	}
	if decision := store.Ingest(event(repositoryA, worktreeA, "test.completed", now)); !decision.Accepted {
		t.Fatalf("first event = %#v", decision)
	}
	if decision := store.Ingest(event(repositoryA, worktreeA, "build.completed", now)); !decision.Accepted {
		t.Fatalf("second event = %#v", decision)
	}
	if inspection := store.Inspect(repositoryA, worktreeA); len(inspection.Selected) != 1 || inspection.Selected[0].Kind != "build.completed" {
		t.Fatalf("event bound = %#v", inspection)
	}
	if decision := store.Ingest(event(repositoryB, worktreeB, "editor.file_opened", now)); !decision.Accepted {
		t.Fatalf("second scope = %#v", decision)
	}
	if inspection := store.Inspect(repositoryA, worktreeA); len(inspection.Selected) != 0 {
		t.Fatalf("scope bound did not evict oldest scope: %#v", inspection)
	}
}

func TestObserveGitRecordsOnlyActualTransitionsAndExpiresItsBaseline(t *testing.T) {
	store := New(Settings{Retention: time.Minute, MaxEvents: 8, MaxScopes: 2}, func() (Permissions, error) { return Permissions{Activity: true}, nil })
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	if decisions := store.ObserveGit(repositoryA, worktreeA, "head-1", "index-1", false); len(decisions) != 1 || decisions[0].Reason != "baseline recorded" {
		t.Fatalf("baseline = %#v", decisions)
	}
	if decisions := store.ObserveGit(repositoryA, worktreeA, "head-1", "index-1", false); len(decisions) != 1 || decisions[0].Reason != "no Git transition" {
		t.Fatalf("unchanged state = %#v", decisions)
	}
	if decisions := store.ObserveGit(repositoryA, worktreeA, "head-1", "index-2", false); len(decisions) != 1 || decisions[0].Reason != "index changed" {
		t.Fatalf("index transition = %#v", decisions)
	}
	if decisions := store.ObserveGit(repositoryA, worktreeA, "head-2", "index-3", true); len(decisions) != 3 {
		t.Fatalf("head/index transition = %#v", decisions)
	}
	signals := store.Provenance(repositoryA, worktreeA).Signals
	if len(signals) != 3 || signals[0].Kind != "git.index_changed" || signals[0].Count != 2 || signals[1].Kind != "git.head_changed" || signals[2].Kind != "git.commit_completed" {
		t.Fatalf("Git transition signals = %#v", signals)
	}
	now = now.Add(2 * time.Minute)
	if decisions := store.ObserveGit(repositoryA, worktreeA, "head-2", "index-3", false); len(decisions) != 1 || decisions[0].Reason != "baseline recorded" {
		t.Fatalf("expired baseline = %#v", decisions)
	}
}

func TestPermissionRecordIsPrivateAndStrict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions.json")
	if err := SavePermissions(path, Permissions{Activity: true}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("permission file mode: info=%v err=%v", info, err)
	}
	permissions, err := LoadPermissions(path)
	if err != nil || !permissions.Activity {
		t.Fatalf("permissions = %#v, err = %v", permissions, err)
	}
	if err := os.WriteFile(path, []byte(`{"activity":true,"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPermissions(path); err == nil {
		t.Fatal("unknown permission field accepted")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPermissions(path); err == nil {
		t.Fatal("public permission file accepted")
	}
}

func event(repository, worktree, kind string, timestamp time.Time) Event {
	return Event{Schema: SchemaVersion, Repository: repository, Worktree: worktree, Source: "shell", Kind: kind, Timestamp: timestamp, Sensitivity: Private}
}

func TestEventKindsStayExplicit(t *testing.T) {
	if len(orderedKinds) != 10 || strings.Join(orderedKinds, ",") == "" {
		t.Fatalf("event families = %#v", orderedKinds)
	}
}

func TestEventSchemaRejectsUnknownFields(t *testing.T) {
	var value Event
	if err := json.Unmarshal([]byte(`{"schema_version":1,"unknown":true}`), &value); err == nil {
		t.Fatal("unknown event field accepted")
	}
}
