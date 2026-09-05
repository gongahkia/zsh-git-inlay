package daemon

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/activity"
	"github.com/gongahkia/zsh-git-inlay/internal/candidate"
	"github.com/gongahkia/zsh-git-inlay/internal/cloud"
	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/grounding"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	"github.com/gongahkia/zsh-git-inlay/internal/learning"
	"github.com/gongahkia/zsh-git-inlay/internal/provider"
	"github.com/gongahkia/zsh-git-inlay/internal/repoctx"
)

func TestObservePublishesAtomicScopedCandidates(t *testing.T) {
	server, socket := testServer(t)
	first := daemonRepository(t, "first.txt")
	second := daemonRepository(t, "second.txt")
	for _, repository := range []string{first, second} {
		reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "observe", CWD: repository})
		if reply.Status != "pending" && reply.Status != "ready" {
			t.Fatalf("observe %s = %#v", repository, reply)
		}
	}
	firstState := daemonSnapshot(t, first)
	secondState := daemonSnapshot(t, second)
	firstRecord := waitForRecord(t, socket, firstState)
	secondRecord := waitForRecord(t, socket, secondState)
	if firstRecord.Repository == secondRecord.Repository || firstRecord.Fingerprint == secondRecord.Fingerprint {
		t.Fatal("candidate records crossed repository scopes")
	}
	if reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: firstState.Fingerprint, Repository: secondState.RepoID, Worktree: secondState.WorktreeID}); reply.Status != "stale" {
		t.Fatalf("cross-scope lookup = %#v", reply)
	}
	if info, err := os.Stat(server.cachePath(firstRecord.Fingerprint)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache permissions: info=%v err=%v", info, err)
	}
	restarted := New(config.Default(), filepath.Join(t.TempDir(), "unused.sock"), server.cacheDir)
	if reply := restarted.observe(first); reply.Status != "ready" {
		t.Fatalf("cold content-addressed cache lookup = %#v", reply)
	}
}

func TestSupersededStateCannotPublish(t *testing.T) {
	server, _ := testServer(t)
	repository := daemonRepository(t, "first.txt")
	first := daemonSnapshot(t, repository)
	if err := os.WriteFile(filepath.Join(repository, "second.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	daemonGit(t, repository, "add", "second.txt")
	server.generate(context.Background(), first, repository, 1)
	server.mu.Lock()
	_, exists := server.cache[first.Fingerprint]
	server.mu.Unlock()
	if exists {
		t.Fatal("superseded candidate was published in memory")
	}
	if _, err := os.Stat(server.cachePath(first.Fingerprint)); !os.IsNotExist(err) {
		t.Fatalf("superseded candidate was published on disk: %v", err)
	}
}

func TestLearningObservesOnlyACompletedCommit(t *testing.T) {
	server, _ := testServer(t)
	store, err := learning.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server.learner = store
	repository := daemonRepository(t, "learning.go")
	if reply := server.prepareLearning(repository); reply.Status != "ready" {
		t.Fatalf("prepare = %#v", reply)
	}
	if reply := server.commitLearning(repository); reply.Status != "ignored" {
		t.Fatalf("aborted commit = %#v", reply)
	}
	before := daemonSnapshot(t, repository)
	server.learningMu.Lock()
	server.prepared[before.WorktreeID] = preparedLearning{repository: before.RepoID, worktree: before.WorktreeID, head: before.Head, candidates: []string{"fix(api): update parser"}, createdAt: time.Now()}
	server.learningMu.Unlock()
	daemonGit(t, repository, "commit", "-qm", "fix(api): update parser")
	if reply := server.commitLearning(repository); reply.Status != "ready" {
		t.Fatalf("completed commit = %#v", reply)
	}
	after, err := gitstate.Snapshot(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.Load(after.RepoID)
	if err != nil || profile.Local.Samples != 1 || profile.Local.Types["fix"] != 1 {
		t.Fatalf("profile=%#v err=%v", profile, err)
	}
	if _, err := store.SetEnabled(after.RepoID, false); err != nil {
		t.Fatal(err)
	}
	if reply := server.learningChanged(repository); reply.Status != "ready" {
		t.Fatalf("disable notification = %#v", reply)
	}
	if err := os.WriteFile(filepath.Join(repository, "second.go"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	daemonGit(t, repository, "add", "second.go")
	second := daemonSnapshot(t, repository)
	server.learningMu.Lock()
	server.prepared[second.WorktreeID] = preparedLearning{repository: second.RepoID, worktree: second.WorktreeID, head: second.Head, createdAt: time.Now()}
	server.learningMu.Unlock()
	daemonGit(t, repository, "commit", "-qm", "feat(api): add second")
	if reply := server.commitLearning(repository); reply.Status != "ignored" {
		t.Fatalf("disabled learning commit = %#v", reply)
	}
	profile, err = store.Load(after.RepoID)
	if err != nil || profile.Local.Samples != 1 {
		t.Fatalf("disabled profile=%#v err=%v", profile, err)
	}
}

func TestLearningClassificationIsBoundedAndConservative(t *testing.T) {
	if origin, _ := classifyLearningCommit("fix(api): update parser", []string{"fix(api): update parser"}); origin != learning.AcceptedPrimary {
		t.Fatalf("primary origin = %q", origin)
	}
	if origin, _ := classifyLearningCommit("fix(api): improve parser", []string{"fix(api): update parser", "fix(api): improve parser"}); origin != learning.AcceptedAlternative {
		t.Fatalf("alternative origin = %q", origin)
	}
	if origin, rejected := classifyLearningCommit("fix(api): change parser", []string{"fix(api): update parser"}); origin != learning.EditedCandidate || rejected != "update" {
		t.Fatalf("edited origin=%q rejected=%q", origin, rejected)
	}
	if origin, _ := classifyLearningCommit("docs(readme): update guide", []string{"fix(api): update parser"}); origin != learning.UserAuthored {
		t.Fatalf("authored origin = %q", origin)
	}
}

func TestExplicitDeterministicFallbackPublishesAfterProviderFailure(t *testing.T) {
	server, _ := testServer(t)
	server.settings.Provider = "ollama"
	server.settings.ProviderModel = "missing"
	server.settings.ProviderFallback = "deterministic"
	server.provider = failingProvider{}
	server.fallback = provider.Deterministic{}
	repository := daemonRepository(t, "fallback.txt")
	state := daemonSnapshot(t, repository)
	server.generate(context.Background(), state, repository, 1)
	server.mu.Lock()
	record, found := server.cache[state.Fingerprint]
	server.mu.Unlock()
	if !found || record.Provider.Name != "deterministic" {
		t.Fatalf("explicit fallback record = %#v found=%t", record, found)
	}
}

func TestCloudGenerationRequiresGrantAndRevocationRejectsCachedCandidate(t *testing.T) {
	server, _ := testServer(t)
	grants, err := cloud.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server.cloud = grants
	capturing := &cloudCapturingProvider{}
	server.provider, server.fallback = capturing, nil
	repository := daemonRepository(t, "cloud.go")
	state := daemonSnapshot(t, repository)
	server.generate(context.Background(), state, repository, 1)
	if capturing.calls != 0 {
		t.Fatal("cloud provider ran without a user grant")
	}
	if _, err := grants.Set("openai", []cloud.ContextClass{cloud.OutputExcerpts}); err != nil {
		t.Fatal(err)
	}
	server.generate(context.Background(), state, repository, 2)
	if capturing.calls != 0 {
		t.Fatal("cloud provider ran without an available granted source")
	}
	if _, err := grants.Set("openai", []cloud.ContextClass{cloud.StagedDiff}); err != nil {
		t.Fatal(err)
	}
	server.generate(context.Background(), state, repository, 3)
	if capturing.calls != 1 || strings.Contains(capturing.request.Context, "recent_subjects") || !strings.Contains(capturing.request.Context, "staged_patch") {
		t.Fatalf("cloud request=%#v calls=%d", capturing.request, capturing.calls)
	}
	server.mu.Lock()
	record, found := server.cache[state.Fingerprint]
	server.mu.Unlock()
	if !found || record.Cloud == nil || !cloud.SameClasses(record.Cloud.Classes, []cloud.ContextClass{cloud.StagedDiff}) || record.Cloud.ContextFingerprint != capturing.request.ContextFingerprint {
		t.Fatalf("cloud record=%#v found=%t calls=%d request=%#v", record, found, capturing.calls, capturing.request)
	}
	if err := grants.Revoke("openai"); err != nil {
		t.Fatal(err)
	}
	if reply := server.lookup(ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID}); reply.Status != "stale" {
		t.Fatalf("revoked cloud lookup=%#v", reply)
	}
	if _, err := os.Stat(server.cachePath(state.Fingerprint)); !os.IsNotExist(err) {
		t.Fatalf("revoked cloud cache remains: %v", err)
	}
}

func TestCloudGrantChangeCancelsInflightBackgroundJob(t *testing.T) {
	server, _ := testServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.mu.Lock()
	server.jobs["cloud-job"] = job{cancel: cancel, id: 1}
	server.mu.Unlock()
	if reply := server.cloudChanged("openai"); reply.Status != "ready" {
		t.Fatalf("cloud change=%#v", reply)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cloud grant change did not cancel in-flight job")
	}
}

func TestGenerationReceivesBoundedContextAndRecordsItsIdentity(t *testing.T) {
	server, _ := testServer(t)
	repository := daemonRepository(t, "context.go")
	state := daemonSnapshot(t, repository)
	capturing := &contextCapturingProvider{}
	server.provider = capturing
	server.fallback = nil
	server.generate(context.Background(), state, repository, 1)
	if capturing.request.Context == "" || capturing.request.ContextFingerprint != state.ContextFingerprint {
		t.Fatalf("provider request = %#v state = %#v", capturing.request, state)
	}
	server.mu.Lock()
	record, found := server.cache[state.Fingerprint]
	server.mu.Unlock()
	if !found || record.ContextFingerprint != state.ContextFingerprint {
		t.Fatalf("cached context identity = %#v found=%t", record, found)
	}
	if len(record.Grounding) != len(record.Candidates) || record.Grounding[0].State != grounding.Grounded {
		t.Fatalf("cached grounding = %#v", record.Grounding)
	}
}

func TestRankingDemotesUnsupportedProviderClaims(t *testing.T) {
	repository := daemonRepository(t, "parser_test.go")
	state := daemonSnapshot(t, repository)
	compiled, err := repoctx.Compile(context.Background(), repository, state, "deterministic")
	if err != nil {
		t.Fatal(err)
	}
	response := provider.Response{Metadata: provider.Deterministic{}.Metadata(), Candidates: []provider.Candidate{
		{Type: "fix", Scope: "payment", Subject: "prevent payment timeout xyz-999", EvidenceIDs: []string{"change:99"}},
		{Type: "test", Scope: "repo", Subject: "cover staged parser tests", EvidenceIDs: []string{"change:0"}},
	}}
	values, reports, err := rankCandidates(response, compiled, "conservative", config.DefaultRepositoryPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Message != "test(repo): cover staged parser tests" || len(reports) != 1 || reports[0].State != grounding.Grounded {
		t.Fatalf("ranked values=%#v reports=%#v", values, reports)
	}
}

func TestObserveRefreshesChangedProviderConfiguration(t *testing.T) {
	server, _ := testServer(t)
	configRoot := t.TempDir()
	path := filepath.Join(configRoot, "zsh-git-inlay", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	if err := os.WriteFile(path, []byte("[provider]\nname = \"ollama\"\nmodel = \"qwen2.5-coder:0.5b\"\n[grounding]\nambiguity = \"quiet\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := server.refreshProvider(); err != nil {
		t.Fatal(err)
	}
	server.providerMu.RLock()
	metadata := server.provider.Metadata()
	server.providerMu.RUnlock()
	if metadata.Name != "ollama" || metadata.Model != "qwen2.5-coder:0.5b" {
		t.Fatalf("provider refresh = %#v", metadata)
	}
	if policy := server.policy(); policy != "quiet" {
		t.Fatalf("grounding policy refresh = %q", policy)
	}
}

func TestActivitySocketRequiresConsentAndMatchingRepositoryScope(t *testing.T) {
	server, socket := testServer(t)
	permissions := activity.Permissions{}
	server.activity = activity.New(activity.DefaultSettings(), func() (activity.Permissions, error) { return permissions, nil })
	repository := daemonRepository(t, "activity.go")
	state := daemonSnapshot(t, repository)
	event := activity.Event{Schema: activity.SchemaVersion, Repository: state.RepoID, Worktree: state.WorktreeID, Source: "shell", Kind: "git.index_changed", Timestamp: time.Now().UTC(), Sensitivity: activity.Private}
	if reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "event", CWD: repository, Event: &event}); reply.Status != "rejected" {
		t.Fatalf("pre-consent event = %#v", reply)
	}
	if reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "activity_inspect", CWD: repository}); reply.Status != "ready" {
		t.Fatalf("pre-consent inspection = %#v", reply)
	} else {
		var inspection activity.Inspection
		if err := json.Unmarshal(reply.Payload, &inspection); err != nil || inspection.Enabled || len(inspection.Selected) != 0 {
			t.Fatalf("pre-consent inspection = %#v err=%v", inspection, err)
		}
	}
	permissions.Activity = true
	if reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "event", CWD: repository, Event: &event}); reply.Status != "accepted" {
		t.Fatalf("consented event = %#v", reply)
	}
	wrongScope := event
	wrongScope.Repository = strings.Repeat("f", 64)
	if reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "event", CWD: repository, Event: &wrongScope}); reply.Status != "rejected" {
		t.Fatalf("cross-scope event = %#v", reply)
	}
	if reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "activity_inspect", CWD: repository}); reply.Status != "ready" {
		t.Fatalf("inspection = %#v", reply)
	} else {
		var inspection activity.Inspection
		if err := json.Unmarshal(reply.Payload, &inspection); err != nil || !inspection.Enabled || len(inspection.Selected) != 1 {
			t.Fatalf("inspection = %#v err=%v", inspection, err)
		}
	}
}

func TestActivitySignalsReachContextAndRevocationInvalidatesLookup(t *testing.T) {
	server, socket := testServer(t)
	permissions := activity.Permissions{Activity: true}
	server.activity = activity.New(activity.DefaultSettings(), func() (activity.Permissions, error) { return permissions, nil })
	repository := daemonRepository(t, "activity.go")
	state := daemonSnapshot(t, repository)
	event := activity.Event{Schema: activity.SchemaVersion, Repository: state.RepoID, Worktree: state.WorktreeID, Source: "shell", Kind: "test.completed", Timestamp: time.Now().UTC(), Sensitivity: activity.Private, Data: map[string]string{"token": "ghp_not_retained"}}
	if reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "event", CWD: repository, Event: &event}); reply.Status != "accepted" {
		t.Fatalf("event = %#v", reply)
	}
	capturing := &contextCapturingProvider{}
	server.provider, server.fallback = capturing, nil
	server.generate(context.Background(), state, repository, 1)
	if !strings.Contains(capturing.request.Context, "activity_signals") || !strings.Contains(capturing.request.Context, "test.completed") || strings.Contains(capturing.request.Context, "ghp_not_retained") {
		t.Fatalf("activity context = %q", capturing.request.Context)
	}
	if reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID}); reply.Status != "ready" {
		t.Fatalf("activity lookup = %#v", reply)
	}
	permissions.Activity = false
	if reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID}); reply.Status != "activity_stale" {
		t.Fatalf("revoked activity lookup = %#v", reply)
	}
}

func TestActivityGitStateReportsOnlyObservedIndexAndHeadChanges(t *testing.T) {
	server, _ := testServer(t)
	server.activity = activity.New(activity.DefaultSettings(), func() (activity.Permissions, error) { return activity.Permissions{Activity: true}, nil })
	repository := daemonRepository(t, "activity.go")
	if reply := server.observeActivityGit(repository, false); reply.Status != "ready" {
		t.Fatalf("initial Git state = %#v", reply)
	}
	if err := os.WriteFile(filepath.Join(repository, "second.go"), []byte("package second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	daemonGit(t, repository, "add", "second.go")
	if reply := server.observeActivityGit(repository, false); reply.Status != "ready" {
		t.Fatalf("index state = %#v", reply)
	}
	daemonGit(t, repository, "commit", "-qm", "feat: establish baseline")
	if reply := server.observeActivityGit(repository, true); reply.Status != "ready" {
		t.Fatalf("head state = %#v", reply)
	}
	state, err := gitstate.Snapshot(context.Background(), repository)
	if err != nil || state.RepoID == "" || state.WorktreeID == "" {
		t.Fatalf("final Git state = %#v err=%v", state, err)
	}
	signals := server.activity.Provenance(state.RepoID, state.WorktreeID).Signals
	if len(signals) != 3 || signals[0].Kind != "git.index_changed" || signals[1].Kind != "git.head_changed" || signals[2].Kind != "git.commit_completed" {
		t.Fatalf("Git state signals = %#v", signals)
	}
}

type failingProvider struct{}

func (failingProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "ollama", Model: "missing", Quantization: "unknown", Runtime: "ollama-local", PromptVersion: "v1"}
}

func (failingProvider) Generate(context.Context, provider.Request) (provider.Response, error) {
	return provider.Response{}, fmt.Errorf("Ollama unavailable")
}

type contextCapturingProvider struct{ request provider.Request }

func (capture *contextCapturingProvider) Metadata() provider.Metadata {
	return provider.Deterministic{}.Metadata()
}

func (capture *contextCapturingProvider) Generate(ctx context.Context, request provider.Request) (provider.Response, error) {
	capture.request = request
	return provider.Deterministic{}.Generate(ctx, request)
}

type cloudCapturingProvider struct {
	request provider.Request
	calls   int
}

func (*cloudCapturingProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "openai", Model: "gpt-5", Quantization: "provider_managed", Runtime: "openai-cloud", PromptVersion: config.ProviderPromptVersion}
}

func (capture *cloudCapturingProvider) Generate(ctx context.Context, request provider.Request) (provider.Response, error) {
	if request.Authorized == nil || !request.Authorized(ctx) {
		return provider.Response{}, fmt.Errorf("cloud request was not authorized")
	}
	capture.request, capture.calls = request, capture.calls+1
	return provider.Response{Metadata: capture.Metadata(), Candidates: []provider.Candidate{{Type: "chore", Scope: "repo", Subject: "update staged cloud", EvidenceIDs: []string{"change:0"}}}}, nil
}

func TestMalformedSocketRequestIsRejected(t *testing.T) {
	_, socket := testServer(t)
	connection, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], ipc.MaxRequestBytes+1)
	if _, err := connection.Write(header[:]); err != nil {
		t.Fatal(err)
	}
	reply, err := ipc.ReadReply(connection)
	if err != nil || reply.Status != "malformed" {
		t.Fatalf("reply = %#v, err = %v", reply, err)
	}
}

func TestTruncatedSocketRequestDoesNotStopDaemon(t *testing.T) {
	_, socket := testServer(t)
	connection, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], 10)
	if _, err := connection.Write(header[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte("{}")); err != nil {
		t.Fatal(err)
	}
	connection.Close()
	if reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "status"}); reply.Status != "ready" {
		t.Fatalf("daemon did not survive truncated request: %#v", reply)
	}
}

func TestConcurrentSessionsAndRapidIndexChanges(t *testing.T) {
	_, socket := testServer(t)
	repository := daemonRepository(t, "state.txt")
	errors := make(chan error, 12)
	for client := 0; client < cap(errors); client++ {
		go func() {
			connection, err := net.Dial("unix", socket)
			if err != nil {
				errors <- err
				return
			}
			defer connection.Close()
			if err := ipc.WriteRequest(connection, ipc.Request{Version: ipc.Version, Operation: "observe", CWD: repository}); err != nil {
				errors <- err
				return
			}
			reply, err := ipc.ReadReply(connection)
			if err != nil || (reply.Status != "pending" && reply.Status != "ready") {
				errors <- fmt.Errorf("observe reply=%#v err=%v", reply, err)
				return
			}
			errors <- nil
		}()
	}
	for client := 0; client < cap(errors); client++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	for revision := 0; revision < 12; revision++ {
		if err := os.WriteFile(filepath.Join(repository, "state.txt"), []byte(fmt.Sprintf("state %d\n", revision)), 0o644); err != nil {
			t.Fatal(err)
		}
		daemonGit(t, repository, "add", "state.txt")
		reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "observe", CWD: repository})
		if reply.Status != "pending" && reply.Status != "ready" {
			t.Fatalf("rapid observe %d = %#v", revision, reply)
		}
	}
	final := daemonSnapshot(t, repository)
	record := waitForRecord(t, socket, final)
	if record.Fingerprint != final.Fingerprint {
		t.Fatalf("final record fingerprint = %s, want %s", record.Fingerprint, final.Fingerprint)
	}
}

func TestServeExitsAfterIdleTimeout(t *testing.T) {
	runtimeDir, cacheDir := filepath.Join(t.TempDir(), "runtime"), filepath.Join(t.TempDir(), "cache")
	t.Setenv("ZSH_GIT_INLAY_RUNTIME_DIR", runtimeDir)
	t.Setenv("ZSH_GIT_INLAY_CACHE_DIR", cacheDir)
	settings := config.Default()
	settings.IdleTimeout = 80 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), settings, "") }()
	socket := filepath.Join(runtimeDir, "daemon.sock")
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if info, err := os.Stat(socket); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket permissions: info=%v err=%v", info, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("daemon did not idle exit")
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("socket survived idle exit: %v", err)
	}
}

func TestPrepareSocketRefusesRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.sock")
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareSocket(path); err == nil {
		t.Fatal("regular file was accepted as a replaceable socket")
	}
}

func TestCacheGarbageCollectionEnforcesCountAndAge(t *testing.T) {
	root := t.TempDir()
	settings := config.Default()
	settings.CacheMaxRecords = 2
	settings.CacheMaxAge = time.Hour
	server := New(settings, filepath.Join(root, "daemon.sock"), root)
	oldest := cacheRecord("a", time.Now().Add(-30*time.Minute))
	middle := cacheRecord("b", time.Now().Add(-20*time.Minute))
	newest := cacheRecord("c", time.Now().Add(-10*time.Minute))
	for _, record := range []Record{oldest, middle, newest} {
		if err := server.store(record); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(server.cachePath(oldest.Fingerprint)); !os.IsNotExist(err) {
		t.Fatalf("oldest record survived count GC: %v", err)
	}
	if _, err := os.Stat(server.cachePath(middle.Fingerprint)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(server.cachePath(newest.Fingerprint)); err != nil {
		t.Fatal(err)
	}
	if status := daemonStatus(t, server); status.Cache.Entries != 2 || status.Cache.CapacityRemoved != 1 {
		t.Fatalf("cache status = %#v", status.Cache)
	}

	expired := cacheRecord("d", time.Now().Add(-2*time.Hour))
	if err := server.store(expired); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(server.cachePath(expired.Fingerprint)); !os.IsNotExist(err) {
		t.Fatalf("expired record survived age GC: %v", err)
	}
	if status := daemonStatus(t, server); status.Cache.ExpiredRemoved != 1 {
		t.Fatalf("expired record was not accounted: %#v", status.Cache)
	}
}

func TestCacheGarbageCollectionRemovesCorruptAndStaleRecords(t *testing.T) {
	root := t.TempDir()
	server := New(config.Default(), filepath.Join(root, "daemon.sock"), root)
	corruptFingerprint := strings.Repeat("d", 64)
	if err := os.WriteFile(server.cachePath(corruptFingerprint), []byte("not JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := server.collectCache(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(server.cachePath(corruptFingerprint)); !os.IsNotExist(err) {
		t.Fatalf("corrupt record survived: %v", err)
	}
	if status := daemonStatus(t, server); status.Cache.CorruptRemoved != 1 {
		t.Fatalf("corrupt record was not accounted: %#v", status.Cache)
	}

	record := cacheRecord("e", time.Now())
	if err := server.store(record); err != nil {
		t.Fatal(err)
	}
	server.discardRecord(record.Fingerprint, true)
	if _, err := os.Stat(server.cachePath(record.Fingerprint)); !os.IsNotExist(err) {
		t.Fatalf("discarded stale record survived: %v", err)
	}
	if status := daemonStatus(t, server); status.Cache.StaleDiscarded != 1 {
		t.Fatalf("stale record was not accounted: %#v", status.Cache)
	}
}

func TestCacheGarbageCollectionEnforcesByteLimit(t *testing.T) {
	root := t.TempDir()
	settings := config.Default()
	settings.CacheMaxRecords = 512
	settings.CacheMaxBytes = 64 * 1024
	server := New(settings, filepath.Join(root, "daemon.sock"), root)
	created := time.Now()
	for index := 0; index < 320; index++ {
		record := cacheRecordFingerprint(fmt.Sprintf("%064x", index), created)
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(server.cachePath(record.Fingerprint), encoded, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.collectCache(); err != nil {
		t.Fatal(err)
	}
	status := daemonStatus(t, server)
	if status.Cache.Bytes > settings.CacheMaxBytes || status.Cache.CapacityRemoved == 0 || status.Cache.Entries >= 320 {
		t.Fatalf("byte limit was not enforced: %#v", status.Cache)
	}
}

func TestCacheDoesNotRememberAnEvictedRecord(t *testing.T) {
	root := t.TempDir()
	settings := config.Default()
	settings.CacheMaxRecords = 1
	server := New(settings, filepath.Join(root, "daemon.sock"), root)
	newer := cacheRecord("a", time.Now())
	if err := server.store(newer); err != nil {
		t.Fatal(err)
	}
	if !server.remember(newer.Fingerprint) {
		t.Fatal("retained record was not remembered")
	}
	older := cacheRecord("b", time.Now().Add(-time.Minute))
	if err := server.store(older); err != nil {
		t.Fatal(err)
	}
	if server.remember(older.Fingerprint) {
		t.Fatal("evicted record was remembered")
	}
	server.mu.Lock()
	_, retained := server.cache[newer.Fingerprint]
	_, evicted := server.cache[older.Fingerprint]
	server.mu.Unlock()
	if !retained || evicted {
		t.Fatalf("memory cache retained=%t evicted=%t", retained, evicted)
	}
}

func cacheRecord(seed string, created time.Time) Record {
	return cacheRecordFingerprint(strings.Repeat(seed, 64), created)
}

func cacheRecordFingerprint(fingerprint string, created time.Time) Record {
	return Record{
		Fingerprint:        fingerprint,
		Repository:         strings.Repeat("r", 64),
		Worktree:           strings.Repeat("w", 64),
		ContextFingerprint: strings.Repeat("c", 64),
		CreatedAt:          created,
		Provider:           provider.Deterministic{}.Metadata(),
		Grounding: []grounding.Result{
			{State: grounding.Grounded},
			{State: grounding.Grounded},
		},
		Candidates: []candidate.Candidate{
			{Message: "chore(repo): update staged files", Rank: 0},
			{Message: "chore(repo): refine staged changes", Rank: 1},
		},
	}
}

func daemonStatus(t *testing.T, server *Server) Status {
	t.Helper()
	var status Status
	if err := json.Unmarshal(server.status().Payload, &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func testServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	socket, cacheDir := filepath.Join(root, "daemon.sock"), filepath.Join(root, "cache")
	if err := os.Mkdir(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	settings := config.Default()
	settings.IdleTimeout = time.Minute
	server := New(settings, socket, cacheDir)
	go func() { _ = server.serve(listener) }()
	t.Cleanup(func() { server.Close(); _ = listener.Close() })
	return server, socket
}

func daemonCall(t *testing.T, socket string, request ipc.Request) ipc.Reply {
	t.Helper()
	connection, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if err := ipc.WriteRequest(connection, request); err != nil {
		t.Fatal(err)
	}
	reply, err := ipc.ReadReply(connection)
	if err != nil {
		t.Fatal(err)
	}
	return reply
}

func waitForRecord(t *testing.T, socket string, state gitstate.State) Record {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reply := daemonCall(t, socket, ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID})
		if reply.Status == "ready" {
			var record Record
			if err := json.Unmarshal(reply.Payload, &record); err != nil {
				t.Fatal(err)
			}
			return record
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("candidate was not prepared")
	return Record{}
}

func daemonRepository(t *testing.T, filename string) string {
	t.Helper()
	repository := t.TempDir()
	daemonGit(t, repository, "init", "-q", "-b", "main")
	daemonGit(t, repository, "config", "user.name", "Test")
	daemonGit(t, repository, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, filename), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	daemonGit(t, repository, "add", filename)
	return repository
}

func daemonSnapshot(t *testing.T, repository string) gitstate.State {
	t.Helper()
	state, err := gitstate.Snapshot(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	if state.Availability != gitstate.Ready {
		t.Fatalf("state = %#v", state)
	}
	return state
}
func daemonGit(t *testing.T, cwd string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
