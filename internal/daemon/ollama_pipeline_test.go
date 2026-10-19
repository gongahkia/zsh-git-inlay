package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	"github.com/gongahkia/zsh-git-inlay/internal/provider"
)

func TestOllamaPipelineRejectsTruncationThenPublishesGroundedRecovery(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ZSH_GIT_INLAY_DATA_DIR", t.TempDir())
	var healthy atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/show" {
			_, _ = writer.Write([]byte(`{"details":{"quantization_level":"fixture"}}`))
			return
		}
		if request.URL.Path != "/api/generate" {
			http.NotFound(writer, request)
			return
		}
		reason := "length"
		if healthy.Load() {
			reason = "stop"
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"done": true, "done_reason": reason, "load_duration": 10, "prompt_eval_count": 20, "eval_count": 30, "response": `{"candidates":[{"type":"chore","scope":"cache.txt","subject":"update cache.txt","evidence_ids":["change:0"]}]}`})
	}))
	defer runtime.Close()
	backend, err := provider.NewOllama(runtime.URL, "fixture-model", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	server, socket := testServer(t)
	server.providerMu.Lock()
	server.provider = backend
	server.fallback = nil
	server.providerMu.Unlock()
	repository := daemonRepository(t, "cache.txt")
	state := daemonSnapshot(t, repository)
	server.observe(repository)
	deadline := time.Now().Add(5 * time.Second)
	for server.failure(state.Fingerprint) == "" {
		if time.Now().After(deadline) {
			t.Fatal("failed generation did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var diagnostic Diagnostic
	if err := json.Unmarshal(server.diagnose(repository).Payload, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if diagnostic.Status != "generation_failed" || !strings.Contains(diagnostic.Reason, "output_truncated") {
		t.Fatalf("diagnostic=%#v", diagnostic)
	}
	lookup := server.lookup(ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID})
	if lookup.Status == "ready" {
		t.Fatal("truncated output reached prepared cache")
	}
	healthy.Store(true)
	if reply := server.observe(repository); reply.Status != "pending" {
		t.Fatalf("recovery=%#v", reply)
	}
	record := waitForRecord(t, socket, state)
	if record.Provider.Name != "ollama" || record.Provider.Generation == nil || record.Provider.Generation.LoadDuration != 10 || record.Provider.Generation.DoneReason != "stop" {
		t.Fatalf("lost generation provenance: %#v", record.Provider)
	}
	if len(record.Candidates) != 1 || record.Candidates[0].Message != "chore(cache.txt): update cache.txt" {
		t.Fatalf("candidates=%#v", record.Candidates)
	}
}
