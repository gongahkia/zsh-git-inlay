package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/candidate"
	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/grounding"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	"github.com/gongahkia/zsh-git-inlay/internal/provider"
)

func BenchmarkWarmLookupRoundTrip(b *testing.B) {
	root := b.TempDir()
	repository := filepath.Join(root, "repository")
	benchmarkDaemonGit(b, root, "init", "-q", "-b", "main", repository)
	if err := os.WriteFile(filepath.Join(repository, "file.txt"), []byte("staged\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	benchmarkDaemonGit(b, repository, "add", "file.txt")
	state, err := gitstate.Snapshot(context.Background(), repository)
	if err != nil || state.Availability != gitstate.Ready {
		b.Fatalf("state=%#v err=%v", state, err)
	}
	socket, cacheDir := filepath.Join(root, "daemon.sock"), filepath.Join(root, "cache")
	if err := os.Mkdir(cacheDir, 0o700); err != nil {
		b.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		b.Fatal(err)
	}
	settings := config.Default()
	settings.IdleTimeout = time.Minute
	server := New(settings, socket, cacheDir)
	go func() { _ = server.serve(listener) }()
	b.Cleanup(func() { server.Close(); _ = listener.Close() })

	candidates, err := candidateForBenchmark(repository)
	if err != nil {
		b.Fatal(err)
	}
	record := Record{Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID, ContextFingerprint: state.ContextFingerprint, Candidates: candidates, CreatedAt: time.Now().UTC(), Provider: provider.Deterministic{}.Metadata(), Grounding: groundedForBenchmark(candidates)}
	if err := server.store(record); err != nil {
		b.Fatal(err)
	}
	server.cache[state.Fingerprint] = record
	request := ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		connection, err := net.Dial("unix", socket)
		if err != nil {
			b.Fatal(err)
		}
		_ = connection.SetDeadline(time.Now().Add(time.Second))
		if err := ipc.WriteRequest(connection, request); err != nil {
			b.Fatal(err)
		}
		reply, err := ipc.ReadReply(connection)
		connection.Close()
		if err != nil || reply.Status != "ready" {
			b.Fatalf("reply=%#v err=%v", reply, err)
		}
	}
}

func candidateForBenchmark(repository string) ([]candidate.Candidate, error) {
	return candidate.Generate(context.Background(), repository)
}

func BenchmarkCacheRecordEncoding(b *testing.B) {
	record := Record{Fingerprint: "fingerprint", Repository: "repository", Worktree: "worktree", ContextFingerprint: "context", Candidates: []candidate.Candidate{{Message: "chore(repo): prepare staged update", Rank: 0}}, CreatedAt: time.Unix(0, 0).UTC(), Provider: provider.Deterministic{}.Metadata(), Grounding: []grounding.Result{{State: grounding.Grounded}}}
	for index := 0; index < b.N; index++ {
		if _, err := json.Marshal(record); err != nil {
			b.Fatal(err)
		}
	}
}

func groundedForBenchmark(candidates []candidate.Candidate) []grounding.Result {
	result := make([]grounding.Result, len(candidates))
	for index := range result {
		result[index].State = grounding.Grounded
	}
	return result
}

func benchmarkDaemonGit(b *testing.B, cwd string, arguments ...string) {
	b.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		b.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
