package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProfileStoresOnlyBoundedStyleAggregates(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository := testRepositoryID("one")
	profile, err := store.Observe(repository, Observation{Subject: "fix(auth): rotate DO-NOT-RETAIN token", Origin: UserAuthored})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "DO-NOT-RETAIN") || profile.Local.Types["fix"] != 1 || profile.Local.Scopes["auth"] != 1 {
		t.Fatalf("profile retained unexpected content: %s", encoded)
	}
	info, err := os.Stat(store.path(repository))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("profile permissions info=%v err=%v", info, err)
	}
}

func TestProfileDisableResetAndExplicitImportRemainRepositoryScoped(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, second := testRepositoryID("first"), testRepositoryID("second")
	if _, err := store.Observe(first, Observation{Subject: "fix(api): update parser", Origin: UserAuthored}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetEnabled(first, false); err != nil {
		t.Fatal(err)
	}
	disabled, err := store.Observe(first, Observation{Subject: "feat(ui): add panel", Origin: UserAuthored})
	if err != nil || disabled.Local.Samples != 1 {
		t.Fatalf("disabled profile=%#v err=%v", disabled, err)
	}
	exported, err := store.Export(first)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := store.Import(second, exported)
	if err != nil || imported.Repository != second || imported.Local.Samples != 1 || imported.Enabled {
		t.Fatalf("imported=%#v err=%v", imported, err)
	}
	if err := store.Reset(first); err != nil {
		t.Fatal(err)
	}
	reset, err := store.Load(first)
	if err != nil || !reset.Enabled || reset.Local.Samples != 0 {
		t.Fatalf("reset=%#v err=%v", reset, err)
	}
}

func TestStatisticsAreBoundedAndDecayRatherThanGrowing(t *testing.T) {
	stats := emptyStats()
	for index := 0; index < maxSamples+32; index++ {
		add(&stats, Observation{Subject: "fix(scope" + string(rune('a'+index%26)) + "): update component", Origin: UserAuthored})
	}
	if stats.Samples > maxSamples || len(stats.Types) > maxTypes || len(stats.Scopes) > maxScopes || len(stats.PreferredVerbs) > maxVerbs {
		t.Fatalf("unbounded statistics: %#v", stats)
	}
}

func TestProfileStorageCapRetainsTheCurrentWrite(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.now = testTime
	var latest string
	for index := 0; index < maxProfiles+1; index++ {
		latest = testRepositoryID(fmt.Sprintf("profile-%d", index))
		if _, err := store.Observe(latest, Observation{Subject: "fix(api): update parser", Origin: UserAuthored}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Load(latest); err != nil {
		t.Fatalf("current profile was evicted: %v", err)
	}
	entries, err := os.ReadDir(store.directory)
	if err != nil || len(entries) != maxProfiles {
		t.Fatalf("profile entries=%d err=%v", len(entries), err)
	}
}

func TestReorderIsExplainableAndCannotRunWhenDisabled(t *testing.T) {
	profile := Profile{Schema: SchemaVersion, Repository: testRepositoryID("rank"), Version: 3, Enabled: true, UpdatedAt: testTime(), Local: emptyStats()}
	profile.Local.Samples = 3
	profile.Local.Types["fix"] = 3
	profile.Local.Scopes["api"] = 3
	profile.Local.PreferredVerbs["fix"] = 3
	messages := []string{"feat(ui): add panel", "fix(api): fix parser"}
	order, explanation := Reorder(messages, profile, emptyStats())
	if order[0] != 1 || len(explanation.Adjustments) != len(messages) || explanation.Adjustments[1].Score <= explanation.Adjustments[0].Score {
		t.Fatalf("order=%v explanation=%#v", order, explanation)
	}
	profile.Enabled = false
	order, explanation = Reorder(messages, profile, emptyStats())
	if order[0] != 0 || explanation.Enabled || len(explanation.Adjustments) != 0 {
		t.Fatalf("disabled order=%v explanation=%#v", order, explanation)
	}
}

func TestExplanationValidationRejectsUntrustedDiagnosticText(t *testing.T) {
	valid := Explanation{Enabled: true, ProfileVersion: 1, Adjustments: []Adjustment{{Index: 0, Score: 2, Reasons: []string{"local_type=fix"}}}}
	if !ValidExplanation(valid, 1) {
		t.Fatal("valid explanation was rejected")
	}
	valid.Adjustments[0].Reasons = []string{"unsafe\nreason"}
	if ValidExplanation(valid, 1) {
		t.Fatal("unsafe explanation reason was accepted")
	}
}

func TestHistoricalAndRemoteNormalizationDiscardRawMetadata(t *testing.T) {
	repository := t.TempDir()
	learningGit(t, repository, "init", "-q", "-b", "main")
	learningGit(t, repository, "config", "user.name", "Test")
	learningGit(t, repository, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "file"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	learningGit(t, repository, "add", "file")
	learningGit(t, repository, "commit", "-qm", "fix(api): update DO-NOT-RETAIN")
	stats, err := Historical(context.Background(), repository)
	if err != nil || stats.Samples != 1 || stats.Types["fix"] != 1 {
		t.Fatalf("historical=%#v err=%v", stats, err)
	}
	encoded, _ := json.Marshal(stats)
	if strings.Contains(string(encoded), "DO-NOT-RETAIN") {
		t.Fatalf("historical statistics retained a subject: %s", encoded)
	}
	first := RemoteDigest("https://name:token@example.invalid/owner/repo.git?private=true#anchor")
	second := RemoteDigest("https://example.invalid/owner/repo")
	if first != second || strings.Contains(NormalizeRemote("https://name:token@example.invalid/owner/repo.git?private=true#anchor"), "token") {
		t.Fatalf("remote normalization leaked credentials")
	}
}

func testRepositoryID(value string) string {
	return RemoteDigest(value)
}

func testTime() (result time.Time) { return time.Unix(1, 0).UTC() }

func learningGit(t *testing.T, cwd string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
