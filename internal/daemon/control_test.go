package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/control"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	"github.com/gongahkia/zsh-git-inlay/internal/provider"
)

func TestControlsImmediatelyGateCachedLookupWithoutNotification(t *testing.T) {
	repository := daemonRepository(t, "controls.go")
	state := daemonSnapshot(t, repository)
	server := New(config.Default(), "", t.TempDir())
	server.controlPath = filepath.Join(t.TempDir(), "data", "controls.json")
	server.generate(context.Background(), state, repository, 1)
	request := ipc.Request{Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID}
	if reply := server.lookup(request); reply.Status != "ready" {
		t.Fatalf("initial generation: %+v", reply)
	}
	for _, operation := range []string{"disable", "enable", "snooze", "enable"} {
		if _, err := control.Set(server.controlPath, state.RepoID, operation, time.Hour); err != nil {
			t.Fatal(err)
		}
		expected := map[string]string{"disable": "disabled", "snooze": "snoozed", "enable": "ready"}[operation]
		if reply := server.lookup(request); reply.Status != expected {
			t.Fatalf("cached lookup after %s: %+v", operation, reply)
		}
	}
	if err := os.WriteFile(server.controlPath, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if reply := server.lookup(request); reply.Status != "controls_invalid" {
		t.Fatalf("corrupt controls reused cached result: %+v", reply)
	}
}

func TestDisableDuringGenerationPreventsPublication(t *testing.T) {
	repository := daemonRepository(t, "controls.go")
	state := daemonSnapshot(t, repository)
	server := New(config.Default(), "", t.TempDir())
	server.controlPath = filepath.Join(t.TempDir(), "data", "controls.json")
	blocked := &controlBlockingProvider{started: make(chan struct{}), resume: make(chan struct{})}
	server.provider, server.fallback = blocked, nil
	done := make(chan struct{})
	go func() { server.generate(context.Background(), state, repository, 1); close(done) }()
	select {
	case <-blocked.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	if _, err := control.Set(server.controlPath, "", "disable", 0); err != nil {
		t.Fatal(err)
	}
	close(blocked.resume)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("generation did not finish")
	}
	if _, err := os.Stat(server.cachePath(state.Fingerprint)); !os.IsNotExist(err) {
		t.Fatalf("disabled in-flight generation published: %v", err)
	}
}

type controlBlockingProvider struct{ started, resume chan struct{} }

func (*controlBlockingProvider) Metadata() provider.Metadata {
	return provider.Deterministic{}.Metadata()
}
func (value *controlBlockingProvider) Generate(ctx context.Context, request provider.Request) (provider.Response, error) {
	close(value.started)
	select {
	case <-ctx.Done():
		return provider.Response{}, ctx.Err()
	case <-value.resume:
		return provider.Response{Metadata: value.Metadata(), Candidates: []provider.Candidate{{Type: "chore", Subject: "update controls.go", EvidenceIDs: []string{"metadata:staged"}}}}, nil
	}
}
