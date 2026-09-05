package daemon

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
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
