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
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/candidate"
	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/grounding"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	"github.com/gongahkia/zsh-git-inlay/internal/provider"
	"github.com/gongahkia/zsh-git-inlay/internal/repoctx"
	"github.com/gongahkia/zsh-git-inlay/internal/runtime"
)

type Record struct {
	Fingerprint        string                `json:"fingerprint"`
	Repository         string                `json:"repository_id"`
	Worktree           string                `json:"worktree_id"`
	ContextFingerprint string                `json:"context_fingerprint"`
	Candidates         []candidate.Candidate `json:"candidates"`
	CreatedAt          time.Time             `json:"created_at"`
	Provider           provider.Metadata     `json:"provider"`
	Grounding          []grounding.Result    `json:"grounding,omitempty"`
}

type Status struct {
	Socket      string      `json:"socket"`
	Ready       int         `json:"ready"`
	Pending     int         `json:"pending"`
	Active      int         `json:"active_repositories"`
	IdleAfter   string      `json:"idle_after"`
	LastObserve string      `json:"last_observe,omitempty"`
	Cache       CacheStatus `json:"cache"`
}

// CacheStatus reports bounded candidate storage without exposing candidate text.
type CacheStatus struct {
	Entries         int   `json:"entries"`
	Bytes           int64 `json:"bytes"`
	MaxEntries      int   `json:"max_entries"`
	MaxBytes        int64 `json:"max_bytes"`
	ExpiredRemoved  int   `json:"expired_removed"`
	CorruptRemoved  int   `json:"corrupt_removed"`
	CapacityRemoved int   `json:"capacity_removed"`
	StaleDiscarded  int   `json:"stale_discarded"`
}

type Server struct {
	settings config.Settings
	socket   string
	cacheDir string

	mu              sync.Mutex
	cacheMu         sync.Mutex
	providerMu      sync.RWMutex
	cache           map[string]Record
	active          map[string]active
	jobs            map[string]job
	nextJob         uint64
	sem             chan struct{}
	lastUse         time.Time
	lastObserve     string
	cacheStatus     CacheStatus
	provider        provider.Provider
	fallback        provider.Provider
	providerVersion string
	ambiguityPolicy string
	stop            chan struct{}
	stopped         sync.Once
}

type active struct {
	cwd         string
	seen        time.Time
	fingerprint string
}

type job struct {
	cancel context.CancelFunc
	id     uint64
}

func New(settings config.Settings, socket, cacheDir string) *Server {
	selected, err := provider.New(settings)
	if err != nil {
		selected = provider.Deterministic{}
	}
	return &Server{settings: settings, socket: socket, cacheDir: cacheDir, cache: map[string]Record{}, active: map[string]active{}, jobs: map[string]job{}, sem: make(chan struct{}, settings.MaxGenerationJobs), lastUse: time.Now(), cacheStatus: CacheStatus{MaxEntries: settings.CacheMaxRecords, MaxBytes: settings.CacheMaxBytes}, provider: selected, fallback: provider.Fallback(settings), providerVersion: settings.Version, ambiguityPolicy: settings.GroundingPolicy, stop: make(chan struct{})}
}

func Serve(ctx context.Context, settings config.Settings, initialCWD string) error {
	socket, err := runtime.SocketPath()
	if err != nil {
		return err
	}
	cacheDir, err := runtime.CacheDir()
	if err != nil {
		return err
	}
	alreadyRunning, err := prepareSocket(socket)
	if err != nil {
		return err
	}
	if alreadyRunning {
		return nil
	}
	previousMask := syscallUmask077()
	listener, err := net.Listen("unix", socket)
	syscallUmask(previousMask)
	if err != nil {
		return fmt.Errorf("listen on daemon socket: %w", err)
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		listener.Close()
		return err
	}
	server := New(settings, socket, cacheDir)
	if err := server.collectCache(); err != nil {
		listener.Close()
		return err
	}
	defer func() { listener.Close(); _ = os.Remove(socket) }()
	go func() { <-ctx.Done(); server.Close() }()
	if initialCWD != "" {
		server.observe(initialCWD)
	}
	return server.serve(listener)
}

func (server *Server) serve(listener net.Listener) error {
	for {
		if unixListener, ok := listener.(*net.UnixListener); ok {
			_ = unixListener.SetDeadline(time.Now().Add(200 * time.Millisecond))
		}
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			if networkError, ok := err.(net.Error); ok && networkError.Timeout() {
				select {
				case <-server.stop:
					return nil
				default:
				}
				server.mu.Lock()
				expired := time.Since(server.lastUse) >= server.settings.IdleTimeout && len(server.jobs) == 0
				server.mu.Unlock()
				if expired {
					return nil
				}
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
	if err != nil {
		_ = ipc.WriteReply(connection, ipc.Reply{Version: ipc.Version, Status: "malformed", Error: err.Error()})
		return
	}
	if request.Version != ipc.Version {
		_ = ipc.WriteReply(connection, ipc.Reply{Version: ipc.Version, Status: "incompatible", Error: "incompatible IPC version"})
		return
	}
	server.mu.Lock()
	server.lastUse = time.Now()
	server.mu.Unlock()
	var reply ipc.Reply
	switch request.Operation {
	case "observe":
		reply = server.observe(request.CWD)
	case "lookup":
		reply = server.lookup(request)
	case "status":
		reply = server.status()
	case "stop":
		reply = ipc.Reply{Version: ipc.Version, Status: "stopping"}
		go server.Close()
	default:
		reply = ipc.Reply{Version: ipc.Version, Status: "malformed", Error: "unsupported operation"}
	}
	_ = ipc.WriteReply(connection, reply)
}

func (server *Server) observe(cwd string) ipc.Reply {
	if cwd == "" || len(cwd) > 4096 {
		return ipc.Reply{Version: ipc.Version, Status: "malformed", Error: "invalid working directory"}
	}
	if err := server.refreshProvider(); err != nil {
		return server.noteObserve(ipc.Reply{Version: ipc.Version, Status: "error", Error: err.Error()})
	}
	snapshotContext, cancel := gitstate.WithTimeout()
	defer cancel()
	state, err := gitstate.Snapshot(snapshotContext, cwd)
	if err != nil {
		return server.noteObserve(ipc.Reply{Version: ipc.Version, Status: "error", Error: err.Error()})
	}
	if state.Availability != gitstate.Ready {
		return server.noteObserve(ipc.Reply{Version: ipc.Version, Status: string(state.Availability), Error: state.Reason})
	}
	cached, cacheErr := server.load(state.Fingerprint)
	server.mu.Lock()
	server.evictLocked()
	previous, seen := server.active[state.Scope()]
	if seen && previous.fingerprint != state.Fingerprint {
		if previousJob, pending := server.jobs[previous.fingerprint]; pending {
			previousJob.cancel()
			delete(server.jobs, previous.fingerprint)
		}
	}
	server.active[state.Scope()] = active{cwd: cwd, seen: time.Now(), fingerprint: state.Fingerprint}
	if record, ready := server.cache[state.Fingerprint]; ready && !server.expired(record) {
		server.mu.Unlock()
		return ipc.Reply{Version: ipc.Version, Status: "ready"}
	}
	delete(server.cache, state.Fingerprint)
	if cacheErr == nil && cached.Repository == state.RepoID && cached.Worktree == state.WorktreeID {
		server.cache[state.Fingerprint] = cached
		server.mu.Unlock()
		return ipc.Reply{Version: ipc.Version, Status: "ready"}
	}
	if _, pending := server.jobs[state.Fingerprint]; pending {
		server.mu.Unlock()
		return ipc.Reply{Version: ipc.Version, Status: "pending"}
	}
	jobContext, cancelJob := context.WithCancel(context.Background())
	server.nextJob++
	jobID := server.nextJob
	server.jobs[state.Fingerprint] = job{cancel: cancelJob, id: jobID}
	server.mu.Unlock()
	go server.generate(jobContext, state, cwd, jobID)
	return ipc.Reply{Version: ipc.Version, Status: "pending"}
}

func (server *Server) generate(ctx context.Context, expected gitstate.State, cwd string, jobID uint64) {
	select {
	case server.sem <- struct{}{}:
		defer func() { <-server.sem }()
	case <-ctx.Done():
		server.finish(expected.Fingerprint, jobID)
		return
	}
	compiled, compileErr := server.compileContext(ctx, cwd, expected)
	if compileErr != nil {
		server.finish(expected.Fingerprint, jobID)
		return
	}
	generated, err := server.generateCandidates(ctx, cwd, compiled)
	if err == nil && ctx.Err() == nil {
		policy := server.policy()
		candidates, reports, rankErr := rankCandidates(generated, compiled, policy)
		if rankErr != nil {
			server.finish(expected.Fingerprint, jobID)
			return
		}
		if len(candidates) == 0 && (policy == "conservative" || policy == "hintable") {
			if fallback, fallbackErr := server.generateFallback(ctx, cwd, compiled, generated.Metadata.Name); fallbackErr == nil {
				generated = fallback
				candidates, reports, rankErr = rankCandidates(generated, compiled, policy)
			}
		}
		if rankErr != nil || len(candidates) == 0 {
			server.finish(expected.Fingerprint, jobID)
			return
		}
		checkContext, cancel := gitstate.WithTimeout()
		current, checkErr := gitstate.Snapshot(checkContext, cwd)
		cancel()
		if checkErr == nil && current.Availability == gitstate.Ready && current.Fingerprint == expected.Fingerprint {
			record := Record{Fingerprint: expected.Fingerprint, Repository: expected.RepoID, Worktree: expected.WorktreeID, ContextFingerprint: expected.ContextFingerprint, Candidates: candidates, CreatedAt: time.Now().UTC(), Provider: generated.Metadata, Grounding: reports}
			if server.store(record) == nil {
				finalContext, finalCancel := gitstate.WithTimeout()
				final, finalErr := gitstate.Snapshot(finalContext, cwd)
				finalCancel()
				if finalErr != nil || final.Availability != gitstate.Ready || final.Fingerprint != expected.Fingerprint {
					server.discardRecord(record.Fingerprint, true)
				} else {
					server.remember(record.Fingerprint)
				}
			}
		}
	}
	server.finish(expected.Fingerprint, jobID)
}

func rankCandidates(response provider.Response, compiled repoctx.Compiled, policy string) ([]candidate.Candidate, []grounding.Result, error) {
	values, err := response.ToCandidates()
	if err != nil {
		return nil, nil, err
	}
	reports := grounding.Evaluate(response.Candidates, compiled)
	order := grounding.Rank(reports, policy)
	rankedValues := make([]candidate.Candidate, 0, len(order))
	rankedReports := make([]grounding.Result, 0, len(order))
	for rank, index := range order {
		value := values[index]
		value.Rank = rank
		rankedValues = append(rankedValues, value)
		rankedReports = append(rankedReports, reports[index])
	}
	return rankedValues, rankedReports, nil
}

func (server *Server) compileContext(ctx context.Context, cwd string, state gitstate.State) (repoctx.Compiled, error) {
	server.providerMu.RLock()
	name := server.provider.Metadata().Name
	server.providerMu.RUnlock()
	return repoctx.Compile(ctx, cwd, state, name)
}

func (server *Server) generateCandidates(ctx context.Context, cwd string, compiled repoctx.Compiled) (provider.Response, error) {
	server.providerMu.RLock()
	selected, fallback := server.provider, server.fallback
	server.providerMu.RUnlock()
	request := provider.Request{CWD: cwd, Context: compiled.Prompt(), ContextFingerprint: compiled.ContextFingerprint}
	response, err := selected.Generate(ctx, request)
	if err == nil || fallback == nil {
		return response, err
	}
	return fallback.Generate(ctx, request)
}

func (server *Server) generateFallback(ctx context.Context, cwd string, compiled repoctx.Compiled, selectedName string) (provider.Response, error) {
	server.providerMu.RLock()
	fallback := server.fallback
	server.providerMu.RUnlock()
	if fallback == nil || fallback.Metadata().Name == selectedName {
		return provider.Response{}, fmt.Errorf("no distinct deterministic fallback is configured")
	}
	return fallback.Generate(ctx, provider.Request{CWD: cwd, Context: compiled.Prompt(), ContextFingerprint: compiled.ContextFingerprint})
}

func (server *Server) policy() string {
	server.providerMu.RLock()
	defer server.providerMu.RUnlock()
	return server.ambiguityPolicy
}

func (server *Server) refreshProvider() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	server.providerMu.RLock()
	current := server.providerVersion
	server.providerMu.RUnlock()
	if settings.Version == current {
		return nil
	}
	selected, err := provider.New(settings)
	if err != nil {
		return err
	}
	server.providerMu.Lock()
	if settings.Version != server.providerVersion {
		server.provider, server.fallback, server.providerVersion, server.ambiguityPolicy = selected, provider.Fallback(settings), settings.Version, settings.GroundingPolicy
	}
	server.providerMu.Unlock()
	return nil
}

func (server *Server) finish(fingerprint string, jobID uint64) {
	server.mu.Lock()
	if current, found := server.jobs[fingerprint]; found && current.id == jobID {
		delete(server.jobs, fingerprint)
	}
	server.mu.Unlock()
}

func (server *Server) lookup(request ipc.Request) ipc.Reply {
	if len(request.Fingerprint) != 64 || len(request.Repository) != 64 || len(request.Worktree) != 64 {
		return ipc.Reply{Version: ipc.Version, Status: "malformed", Error: "invalid fingerprint scope"}
	}
	server.mu.Lock()
	record, found := server.cache[request.Fingerprint]
	expired := false
	if found && server.expired(record) {
		delete(server.cache, request.Fingerprint)
		found = false
		expired = true
	}
	server.mu.Unlock()
	if expired {
		server.discardExpired(request.Fingerprint)
	}
	if !found {
		loaded, err := server.load(request.Fingerprint)
		if err == nil {
			record, found = loaded, true
			server.mu.Lock()
			server.cache[record.Fingerprint] = record
			server.mu.Unlock()
		}
	}
	if !found {
		return ipc.Reply{Version: ipc.Version, Status: "pending"}
	}
	if record.Repository != request.Repository || record.Worktree != request.Worktree {
		return ipc.Reply{Version: ipc.Version, Status: "stale"}
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return ipc.Reply{Version: ipc.Version, Status: "error", Error: err.Error()}
	}
	return ipc.Reply{Version: ipc.Version, Status: "ready", Payload: payload}
}

func (server *Server) status() ipc.Reply {
	server.cacheMu.Lock()
	cacheStatus := server.cacheStatus
	server.cacheMu.Unlock()
	server.mu.Lock()
	status := Status{Socket: server.socket, Ready: len(server.cache), Pending: len(server.jobs), Active: len(server.active), IdleAfter: server.settings.IdleTimeout.String(), LastObserve: server.lastObserve, Cache: cacheStatus}
	server.mu.Unlock()
	payload, _ := json.Marshal(status)
	return ipc.Reply{Version: ipc.Version, Status: "ready", Payload: payload}
}

func (server *Server) noteObserve(reply ipc.Reply) ipc.Reply {
	server.mu.Lock()
	server.lastObserve = reply.Status
	if reply.Error != "" {
		server.lastObserve += ": " + reply.Error
	}
	server.mu.Unlock()
	return reply
}

func (server *Server) Close() { server.stopped.Do(func() { close(server.stop) }) }

func (server *Server) evictLocked() {
	if len(server.active) < server.settings.MaxRepositories {
		return
	}
	type pair struct {
		scope string
		seen  time.Time
	}
	pairs := make([]pair, 0, len(server.active))
	for scope, active := range server.active {
		pairs = append(pairs, pair{scope, active.seen})
	}
	sort.Slice(pairs, func(left, right int) bool { return pairs[left].seen.Before(pairs[right].seen) })
	oldest := pairs[0]
	if current, ok := server.active[oldest.scope]; ok {
		if runningJob, running := server.jobs[current.fingerprint]; running {
			runningJob.cancel()
			delete(server.jobs, current.fingerprint)
		}
	}
	delete(server.active, oldest.scope)
}

func (server *Server) cachePath(fingerprint string) string {
	return filepath.Join(server.cacheDir, fingerprint+".json")
}

func (server *Server) store(record Record) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(encoded) > ipc.MaxReplyBytes {
		return fmt.Errorf("candidate record exceeds IPC response limit")
	}
	temporary, err := os.CreateTemp(server.cacheDir, ".candidate-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(encoded)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(name, server.cachePath(record.Fingerprint)); err != nil {
		return err
	}
	return server.collectCache()
}

func (server *Server) load(fingerprint string) (Record, error) {
	server.cacheMu.Lock()
	defer server.cacheMu.Unlock()
	return server.loadLocked(fingerprint)
}

func (server *Server) loadLocked(fingerprint string) (Record, error) {
	var record Record
	content, err := os.ReadFile(server.cachePath(fingerprint))
	if err != nil {
		return record, err
	}
	if len(content) > ipc.MaxReplyBytes {
		server.removeCachePathLocked(fingerprint, &server.cacheStatus.CorruptRemoved)
		return record, fmt.Errorf("candidate cache record too large")
	}
	if err = json.Unmarshal(content, &record); err != nil {
		server.removeCachePathLocked(fingerprint, &server.cacheStatus.CorruptRemoved)
		return record, err
	}
	if !validRecord(record, fingerprint) {
		server.removeCachePathLocked(fingerprint, &server.cacheStatus.CorruptRemoved)
		return Record{}, fmt.Errorf("invalid candidate cache record")
	}
	if server.expired(record) {
		server.removeCachePathLocked(fingerprint, &server.cacheStatus.ExpiredRemoved)
		return Record{}, os.ErrNotExist
	}
	return record, nil
}

func (server *Server) collectCache() error {
	server.cacheMu.Lock()
	defer server.cacheMu.Unlock()
	kept, err := server.collectCacheLocked()
	if err != nil {
		return err
	}
	server.mu.Lock()
	for fingerprint := range server.cache {
		if !kept[fingerprint] {
			delete(server.cache, fingerprint)
		}
	}
	server.mu.Unlock()
	return nil
}

// remember loads a retained on-disk record while holding cacheMu, so a
// collector cannot evict it between validation and insertion into memory.
func (server *Server) remember(fingerprint string) bool {
	server.cacheMu.Lock()
	defer server.cacheMu.Unlock()
	record, err := server.loadLocked(fingerprint)
	if err != nil {
		return false
	}
	server.mu.Lock()
	server.cache[fingerprint] = record
	server.mu.Unlock()
	return true
}

type cacheFile struct {
	fingerprint string
	created     time.Time
	size        int64
}

func (server *Server) collectCacheLocked() (map[string]bool, error) {
	entries, err := os.ReadDir(server.cacheDir)
	if err != nil {
		return nil, fmt.Errorf("read candidate cache: %w", err)
	}
	now := time.Now()
	files := make([]cacheFile, 0, len(entries))
	for _, entry := range entries {
		fingerprint, ok := cacheFilename(entry.Name())
		if !ok {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > ipc.MaxReplyBytes {
			server.removeCachePathLocked(fingerprint, &server.cacheStatus.CorruptRemoved)
			continue
		}
		content, readErr := os.ReadFile(server.cachePath(fingerprint))
		var record Record
		if readErr != nil || json.Unmarshal(content, &record) != nil || !validRecord(record, fingerprint) {
			server.removeCachePathLocked(fingerprint, &server.cacheStatus.CorruptRemoved)
			continue
		}
		created := record.CreatedAt
		if created.IsZero() {
			created = info.ModTime()
		}
		if now.Sub(created) > server.settings.CacheMaxAge {
			server.removeCachePathLocked(fingerprint, &server.cacheStatus.ExpiredRemoved)
			continue
		}
		files = append(files, cacheFile{fingerprint: fingerprint, created: created, size: info.Size()})
	}
	sort.Slice(files, func(left, right int) bool {
		if files[left].created.Equal(files[right].created) {
			return files[left].fingerprint < files[right].fingerprint
		}
		return files[left].created.Before(files[right].created)
	})
	kept := make(map[string]bool, len(files))
	var bytes int64
	for _, file := range files {
		bytes += file.size
	}
	for index, file := range files {
		remaining := len(files) - index
		if remaining > server.settings.CacheMaxRecords || bytes > server.settings.CacheMaxBytes {
			server.removeCachePathLocked(file.fingerprint, &server.cacheStatus.CapacityRemoved)
			bytes -= file.size
			continue
		}
		kept[file.fingerprint] = true
	}
	server.cacheStatus.Entries = len(kept)
	server.cacheStatus.Bytes = bytes
	return kept, nil
}

func (server *Server) discardRecord(fingerprint string, stale bool) {
	server.cacheMu.Lock()
	if stale {
		server.removeCachePathLocked(fingerprint, &server.cacheStatus.StaleDiscarded)
	} else {
		server.removeCachePathLocked(fingerprint, &server.cacheStatus.ExpiredRemoved)
	}
	server.cacheMu.Unlock()
	server.mu.Lock()
	delete(server.cache, fingerprint)
	server.mu.Unlock()
}

func (server *Server) discardExpired(fingerprint string) { server.discardRecord(fingerprint, false) }

func (server *Server) removeCachePathLocked(fingerprint string, counter *int) {
	info, _ := os.Lstat(server.cachePath(fingerprint))
	if err := os.Remove(server.cachePath(fingerprint)); err == nil {
		*counter++
		if server.cacheStatus.Entries > 0 {
			server.cacheStatus.Entries--
		}
		if info != nil && info.Mode().IsRegular() {
			server.cacheStatus.Bytes -= info.Size()
			if server.cacheStatus.Bytes < 0 {
				server.cacheStatus.Bytes = 0
			}
		}
	}
}

func (server *Server) expired(record Record) bool {
	return record.CreatedAt.IsZero() || time.Since(record.CreatedAt) > server.settings.CacheMaxAge
}

func validRecord(record Record, fingerprint string) bool {
	if record.Fingerprint != fingerprint || record.Repository == "" || record.Worktree == "" || len(record.ContextFingerprint) != 64 || record.CreatedAt.IsZero() || !record.Provider.Valid() || len(record.Candidates) == 0 || len(record.Candidates) > candidate.MaxCandidates || len(record.Grounding) != len(record.Candidates) {
		return false
	}
	for index, value := range record.Candidates {
		if value.Rank != index || !candidate.Valid(value) {
			return false
		}
		switch record.Grounding[index].State {
		case grounding.Grounded, grounding.PartiallyGrounded, grounding.Ungrounded, grounding.InsufficientContext:
		default:
			return false
		}
	}
	return true
}

func cacheFilename(name string) (string, bool) {
	if !strings.HasSuffix(name, ".json") || len(name) != 69 {
		return "", false
	}
	fingerprint := strings.TrimSuffix(name, ".json")
	for _, character := range fingerprint {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return "", false
		}
	}
	return fingerprint, true
}

func prepareSocket(socket string) (bool, error) {
	info, err := os.Lstat(socket)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return false, fmt.Errorf("refusing to replace non-socket %s", socket)
	}
	connection, dialErr := net.DialTimeout("unix", socket, 20*time.Millisecond)
	if dialErr == nil {
		connection.Close()
		return true, nil
	}
	if err := os.Remove(socket); err != nil {
		return false, err
	}
	return false, nil
}
