// Package daemon owns background preparation and content-addressed candidates.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/candidate"
	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	"github.com/gongahkia/zsh-git-inlay/internal/runtime"
)

type Record struct {
	Fingerprint string                `json:"fingerprint"`
	Repository  string                `json:"repository_id"`
	Worktree    string                `json:"worktree_id"`
	Candidates  []candidate.Candidate `json:"candidates"`
	CreatedAt   time.Time             `json:"created_at"`
}

type Status struct {
	Socket    string `json:"socket"`
	Ready     int    `json:"ready"`
	Pending   int    `json:"pending"`
	Active    int    `json:"active_repositories"`
	IdleAfter string `json:"idle_after"`
}

type Server struct {
	settings config.Settings
	socket   string
	cacheDir string

	mu      sync.Mutex
	cache   map[string]Record
	active  map[string]active
	jobs    map[string]context.CancelFunc
	sem     chan struct{}
	lastUse time.Time
	stop    chan struct{}
	stopped sync.Once
}

type active struct { cwd string; seen time.Time; fingerprint string }

func New(settings config.Settings, socket, cacheDir string) *Server {
	return &Server{settings: settings, socket: socket, cacheDir: cacheDir, cache: map[string]Record{}, active: map[string]active{}, jobs: map[string]context.CancelFunc{}, sem: make(chan struct{}, settings.MaxGenerationJobs), lastUse: time.Now(), stop: make(chan struct{})}
}

func Serve(ctx context.Context, settings config.Settings) error {
	socket, err := runtime.SocketPath()
	if err != nil { return err }
	cacheDir, err := runtime.CacheDir()
	if err != nil { return err }
	if err := removeSocket(socket); err != nil { return err }
	previousMask := syscallUmask077()
	listener, err := net.Listen("unix", socket)
	syscallUmask(previousMask)
	if err != nil { return fmt.Errorf("listen on daemon socket: %w", err) }
	if err := os.Chmod(socket, 0o600); err != nil { listener.Close(); return err }
	server := New(settings, socket, cacheDir)
	defer func() { listener.Close(); _ = os.Remove(socket) }()
	go func() { <-ctx.Done(); server.Close() }()
	return server.serve(listener)
}

func (server *Server) serve(listener net.Listener) error {
	idle := time.NewTicker(minDuration(server.settings.IdleTimeout/4, time.Second))
	defer idle.Stop()
	for {
		if unixListener, ok := listener.(*net.UnixListener); ok { _ = unixListener.SetDeadline(time.Now().Add(200 * time.Millisecond)) }
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) { return nil }
			if networkError, ok := err.(net.Error); ok && networkError.Timeout() {
				select { case <-server.stop: return nil; default: }
				server.mu.Lock(); expired := time.Since(server.lastUse) >= server.settings.IdleTimeout && len(server.jobs) == 0; server.mu.Unlock()
				if expired { return nil }
				continue
			}
			continue
		}
		go server.handle(connection)
	}
}

func (server *Server) handle(connection net.Conn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	request, err := ipc.ReadRequest(io.LimitReader(connection, ipc.MaxRequestBytes+4))
	if err != nil { _ = ipc.WriteReply(connection, ipc.Reply{Version: ipc.Version, Status: "malformed", Error: err.Error()}); return }
	if request.Version != ipc.Version { _ = ipc.WriteReply(connection, ipc.Reply{Version: ipc.Version, Status: "incompatible", Error: "incompatible IPC version"}); return }
	server.mu.Lock(); server.lastUse = time.Now(); server.mu.Unlock()
	var reply ipc.Reply
	switch request.Operation {
	case "observe": reply = server.observe(request.CWD)
	case "lookup": reply = server.lookup(request)
	case "status": reply = server.status()
	case "stop": reply = ipc.Reply{Version: ipc.Version, Status: "stopping"}; go server.Close()
	default: reply = ipc.Reply{Version: ipc.Version, Status: "malformed", Error: "unsupported operation"}
	}
	_ = ipc.WriteReply(connection, reply)
}

func (server *Server) observe(cwd string) ipc.Reply {
	if cwd == "" || len(cwd) > 4096 { return ipc.Reply{Version: ipc.Version, Status: "malformed", Error: "invalid working directory"} }
	context, cancel := gitstate.WithTimeout(); defer cancel()
	state, err := gitstate.Snapshot(context, cwd)
	if err != nil { return ipc.Reply{Version: ipc.Version, Status: "error", Error: err.Error()} }
	if state.Availability != gitstate.Ready { return ipc.Reply{Version: ipc.Version, Status: string(state.Availability), Error: state.Reason} }
	server.mu.Lock()
	server.evictLocked()
	server.active[state.Scope()] = active{cwd: cwd, seen: time.Now(), fingerprint: state.Fingerprint}
	if _, ready := server.cache[state.Fingerprint]; ready { server.mu.Unlock(); return ipc.Reply{Version: ipc.Version, Status: "ready"} }
	if _, pending := server.jobs[state.Fingerprint]; pending { server.mu.Unlock(); return ipc.Reply{Version: ipc.Version, Status: "pending"} }
	for fingerprint, cancelJob := range server.jobs {
		if record, exists := server.active[state.Scope()]; exists && fingerprint != state.Fingerprint && record.fingerprint == state.Fingerprint { cancelJob(); delete(server.jobs, fingerprint) }
	}
	jobContext, cancelJob := context.WithCancel(context.Background())
	server.jobs[state.Fingerprint] = cancelJob
	server.mu.Unlock()
	go server.generate(jobContext, state, cwd)
	return ipc.Reply{Version: ipc.Version, Status: "pending"}
}

func (server *Server) generate(ctx context.Context, expected gitstate.State, cwd string) {
	select { case server.sem <- struct{}{}: defer func() { <-server.sem }(); case <-ctx.Done(): server.finish(expected.Fingerprint); return }
	candidates, err := candidate.Generate(ctx, cwd)
	if err == nil && ctx.Err() == nil {
		checkContext, cancel := gitstate.WithTimeout()
		current, checkErr := gitstate.Snapshot(checkContext, cwd)
		cancel()
		if checkErr == nil && current.Availability == gitstate.Ready && current.Fingerprint == expected.Fingerprint {
			record := Record{Fingerprint: expected.Fingerprint, Repository: expected.RepoID, Worktree: expected.WorktreeID, Candidates: candidates, CreatedAt: time.Now().UTC()}
			if server.store(record) == nil {
				server.mu.Lock(); server.cache[record.Fingerprint] = record; server.mu.Unlock()
			}
		}
	}
	server.finish(expected.Fingerprint)
}

func (server *Server) finish(fingerprint string) { server.mu.Lock(); delete(server.jobs, fingerprint); server.mu.Unlock() }

func (server *Server) lookup(request ipc.Request) ipc.Reply {
	if len(request.Fingerprint) != 64 || len(request.Repository) != 64 || len(request.Worktree) != 64 { return ipc.Reply{Version: ipc.Version, Status: "malformed", Error: "invalid fingerprint scope"} }
	server.mu.Lock()
	record, found := server.cache[request.Fingerprint]
	server.mu.Unlock()
	if !found {
		loaded, err := server.load(request.Fingerprint)
		if err == nil { record, found = loaded, true; server.mu.Lock(); server.cache[record.Fingerprint] = record; server.mu.Unlock() }
	}
	if !found { return ipc.Reply{Version: ipc.Version, Status: "pending"} }
	if record.Repository != request.Repository || record.Worktree != request.Worktree { return ipc.Reply{Version: ipc.Version, Status: "stale"} }
	payload, err := json.Marshal(record)
	if err != nil { return ipc.Reply{Version: ipc.Version, Status: "error", Error: err.Error()} }
	return ipc.Reply{Version: ipc.Version, Status: "ready", Payload: payload}
}

func (server *Server) status() ipc.Reply {
	server.mu.Lock()
	status := Status{Socket: server.socket, Ready: len(server.cache), Pending: len(server.jobs), Active: len(server.active), IdleAfter: server.settings.IdleTimeout.String()}
	server.mu.Unlock()
	payload, _ := json.Marshal(status)
	return ipc.Reply{Version: ipc.Version, Status: "ready", Payload: payload}
}

func (server *Server) Close() { server.stopped.Do(func() { close(server.stop) }) }

func (server *Server) evictLocked() {
	if len(server.active) < server.settings.MaxRepositories { return }
	type pair struct { scope string; seen time.Time }
	pairs := make([]pair, 0, len(server.active))
	for scope, active := range server.active { pairs = append(pairs, pair{scope, active.seen}) }
	sort.Slice(pairs, func(left, right int) bool { return pairs[left].seen.Before(pairs[right].seen) })
	oldest := pairs[0]
	if current, ok := server.active[oldest.scope]; ok { if cancel, running := server.jobs[current.fingerprint]; running { cancel(); delete(server.jobs, current.fingerprint) } }
	delete(server.active, oldest.scope)
}

func (server *Server) cachePath(fingerprint string) string { return filepath.Join(server.cacheDir, fingerprint+".json") }

func (server *Server) store(record Record) error {
	encoded, err := json.Marshal(record)
	if err != nil { return err }
	if len(encoded) > ipc.MaxReplyBytes { return fmt.Errorf("candidate record exceeds IPC response limit") }
	temporary, err := os.CreateTemp(server.cacheDir, ".candidate-")
	if err != nil { return err }
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(0o600); err == nil { _, err = temporary.Write(encoded) }
	if err == nil { err = temporary.Sync() }
	if closeErr := temporary.Close(); err == nil { err = closeErr }
	if err != nil { return err }
	return os.Rename(name, server.cachePath(record.Fingerprint))
}

func (server *Server) load(fingerprint string) (Record, error) {
	var record Record
	content, err := os.ReadFile(server.cachePath(fingerprint))
	if err != nil { return record, err }
	if len(content) > ipc.MaxReplyBytes { return record, fmt.Errorf("candidate cache record too large") }
	if err = json.Unmarshal(content, &record); err != nil { return record, err }
	if record.Fingerprint != fingerprint || len(record.Candidates) == 0 || len(record.Candidates) > candidate.MaxCandidates { return Record{}, fmt.Errorf("invalid candidate cache record") }
	return record, nil
}

func removeSocket(socket string) error {
	info, err := os.Lstat(socket)
	if os.IsNotExist(err) { return nil }
	if err != nil { return err }
	if info.Mode()&os.ModeSocket == 0 { return fmt.Errorf("refusing to replace non-socket %s", socket) }
	return os.Remove(socket)
}

func minDuration(first, second time.Duration) time.Duration { if first < second { return first }; return second }
