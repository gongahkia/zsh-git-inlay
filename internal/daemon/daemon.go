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
	"github.com/gongahkia/zsh-git-inlay/internal/runtime"
)

type Record struct {
	Fingerprint        string                  `json:"fingerprint"`
	Repository         string                  `json:"repository_id"`
	Worktree           string                  `json:"worktree_id"`
	ContextFingerprint string                  `json:"context_fingerprint"`
	Candidates         []candidate.Candidate   `json:"candidates"`
	CreatedAt          time.Time               `json:"created_at"`
	Provider           provider.Metadata       `json:"provider"`
	Policy             config.RepositoryPolicy `json:"policy"`
	Grounding          []grounding.Result      `json:"grounding,omitempty"`
	Activity           activity.Provenance     `json:"activity,omitempty"`
	Learning           learning.Explanation    `json:"learning,omitempty"`
	Cloud              *CloudProvenance        `json:"cloud,omitempty"`
}

// CloudProvenance binds a cached candidate to one provider and the exact
// consented context policy used for its remote request. It has no source text
// or credential material.
type CloudProvenance struct {
	Provider           string               `json:"provider"`
	Classes            []cloud.ContextClass `json:"context_classes"`
	ContextFingerprint string               `json:"context_fingerprint"`
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

	mu               sync.Mutex
	cacheMu          sync.Mutex
	providerMu       sync.RWMutex
	cache            map[string]Record
	active           map[string]active
	jobs             map[string]job
	nextJob          uint64
	sem              chan struct{}
	lastUse          time.Time
	lastObserve      string
	cacheStatus      CacheStatus
	provider         provider.Provider
	fallback         provider.Provider
	providerVersion  string
	ambiguityPolicy  string
	activity         *activity.Store
	learner          *learning.Store
	cloud            *cloud.Store
	learningMu       sync.Mutex
	learningProfiles map[string]learning.Profile
	prepared         map[string]preparedLearning
	stop             chan struct{}
	stopped          sync.Once
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

type preparedLearning struct {
	repository string
	worktree   string
	head       string
	candidates []string
	createdAt  time.Time
}

func New(settings config.Settings, socket, cacheDir string) *Server {
	return newServer(settings, socket, cacheDir, activity.New(activitySettings(settings), func() (activity.Permissions, error) {
		return activity.Permissions{}, nil
	}))
}

func newServer(settings config.Settings, socket, cacheDir string, events *activity.Store) *Server {
	return newServerWithLearning(settings, socket, cacheDir, events, nil)
}

func newServerWithLearning(settings config.Settings, socket, cacheDir string, events *activity.Store, learner *learning.Store) *Server {
	return newServerWithCloud(settings, socket, cacheDir, events, learner, nil)
}

func newServerWithCloud(settings config.Settings, socket, cacheDir string, events *activity.Store, learner *learning.Store, grants *cloud.Store) *Server {
	selected, err := provider.New(settings)
	if err != nil {
		selected = provider.Deterministic{}
	}
	return &Server{settings: settings, socket: socket, cacheDir: cacheDir, cache: map[string]Record{}, active: map[string]active{}, jobs: map[string]job{}, sem: make(chan struct{}, settings.MaxGenerationJobs), lastUse: time.Now(), cacheStatus: CacheStatus{MaxEntries: settings.CacheMaxRecords, MaxBytes: settings.CacheMaxBytes}, provider: selected, fallback: provider.Fallback(settings), providerVersion: settings.Version, ambiguityPolicy: settings.GroundingPolicy, activity: events, learner: learner, cloud: grants, learningProfiles: map[string]learning.Profile{}, prepared: map[string]preparedLearning{}, stop: make(chan struct{})}
}

func activitySettings(settings config.Settings) activity.Settings {
	return activity.Settings{Retention: settings.ActivityRetention, MaxEvents: settings.ActivityMaxEvents, MaxScopes: settings.MaxRepositories}
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
	dataDir, dataErr := runtime.DataDir()
	permissionsPath := ""
	if dataErr == nil {
		permissionsPath = activity.PermissionsPath(dataDir)
	}
	events := activity.New(activitySettings(settings), func() (activity.Permissions, error) {
		if permissionsPath == "" {
			return activity.Permissions{}, fmt.Errorf("activity permission storage is unavailable")
		}
		return activity.LoadPermissions(permissionsPath)
	})
	var learner *learning.Store
	var grants *cloud.Store
	if dataErr == nil {
		learner, _ = learning.New(filepath.Join(dataDir, "learning"))
		grants, _ = cloud.New(dataDir)
	}
	server := newServerWithCloud(settings, socket, cacheDir, events, learner, grants)
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
	case "event":
		reply = server.ingestActivity(request)
	case "activity_inspect":
		reply = server.inspectActivity(request.CWD)
	case "activity_clear":
		reply = server.clearActivity(request.CWD)
	case "activity_git_state":
		reply = server.observeActivityGit(request.CWD, request.GitCommit)
	case "learning_prepare":
		reply = server.prepareLearning(request.CWD)
	case "learning_commit":
		reply = server.commitLearning(request.CWD)
	case "learning_changed":
		reply = server.learningChanged(request.CWD)
	case "cloud_changed":
		reply = server.cloudChanged(request.Provider)
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
	activityProvenance := server.activity.Provenance(state.RepoID, state.WorktreeID)
	cached, cacheErr := server.load(state.Fingerprint)
	if cacheErr == nil && cached.Cloud != nil && !server.cloudAllowed(*cached.Cloud) {
		server.discardRecord(state.Fingerprint, true)
		cacheErr = os.ErrNotExist
	}
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
	if record, ready := server.cache[state.Fingerprint]; ready && !server.expired(record) && matchesActivity(record, activityProvenance) && (record.Cloud == nil || server.cloudAllowed(*record.Cloud)) {
		server.mu.Unlock()
		return ipc.Reply{Version: ipc.Version, Status: "ready"}
	}
	delete(server.cache, state.Fingerprint)
	if cacheErr == nil && cached.Repository == state.RepoID && cached.Worktree == state.WorktreeID && matchesActivity(cached, activityProvenance) {
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
	activityProvenance := server.activity.Provenance(expected.RepoID, expected.WorktreeID)
	compiled, compileErr := server.compileContext(ctx, cwd, expected, activityProvenance.Signals)
	if compileErr != nil {
		server.finish(expected.Fingerprint, jobID)
		return
	}
	repositoryPolicy, policyErr := config.LoadRepositoryPolicy(expected.Root)
	if policyErr != nil {
		server.finish(expected.Fingerprint, jobID)
		return
	}
	generated, cloudProvenance, err := server.generateCandidates(ctx, cwd, expected, compiled)
	if err == nil && ctx.Err() == nil {
		policy := server.policy()
		candidates, reports, rankErr := rankCandidates(generated, compiled, policy, repositoryPolicy)
		if rankErr != nil {
			server.finish(expected.Fingerprint, jobID)
			return
		}
		if len(candidates) == 0 && (policy == "conservative" || policy == "hintable") {
			if fallback, fallbackErr := server.generateFallback(ctx, cwd, compiled, generated.Metadata.Name); fallbackErr == nil {
				generated = fallback
				candidates, reports, rankErr = rankCandidates(generated, compiled, policy, repositoryPolicy)
			}
		}
		if rankErr != nil || len(candidates) == 0 {
			server.finish(expected.Fingerprint, jobID)
			return
		}
		candidates, reports, learningExplanation := server.applyLearning(ctx, cwd, expected, candidates, reports)
		checkContext, cancel := gitstate.WithTimeout()
		current, checkErr := gitstate.Snapshot(checkContext, cwd)
		cancel()
		if checkErr == nil && current.Availability == gitstate.Ready && current.Fingerprint == expected.Fingerprint {
			record := Record{Fingerprint: expected.Fingerprint, Repository: expected.RepoID, Worktree: expected.WorktreeID, ContextFingerprint: expected.ContextFingerprint, Candidates: candidates, CreatedAt: time.Now().UTC(), Provider: generated.Metadata, Policy: repositoryPolicy, Grounding: reports, Activity: activityProvenance, Learning: learningExplanation, Cloud: cloudProvenance}
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

func rankCandidates(response provider.Response, compiled repoctx.Compiled, policy string, repositoryPolicy config.RepositoryPolicy) ([]candidate.Candidate, []grounding.Result, error) {
	values, err := response.ToCandidates()
	if err != nil {
		return nil, nil, err
	}
	reports := grounding.Evaluate(response.Candidates, compiled, repositoryPolicy)
	order := grounding.Rank(reports, policy)
	if len(order) == 0 && repositoryPolicy.Body == "required" {
		// Keep only independently safe subject inputs for the secondary compose
		// workflow. suggest suppresses this record, so no subject-only candidate
		// can render under a required-body policy.
		for index, report := range reports {
			if grounding.EligibleForBodyComposition(report) {
				order = append(order, index)
			}
		}
	}
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

// applyLearning runs only after repository policy and grounding have rejected
// unsafe candidates. It can therefore reorder valid candidates but cannot make
// a profile authorize a type, scope, body, or unsupported claim.
func (server *Server) applyLearning(ctx context.Context, cwd string, state gitstate.State, candidates []candidate.Candidate, reports []grounding.Result) ([]candidate.Candidate, []grounding.Result, learning.Explanation) {
	profile, found := server.learningProfile(state.RepoID)
	if !found {
		return candidates, reports, learning.Explanation{}
	}
	historical, err := learning.Historical(ctx, cwd)
	if err != nil {
		historical = learning.Stats{}
	}
	messages := make([]string, len(candidates))
	for index, value := range candidates {
		messages[index] = value.Message
	}
	order, explanation := learning.Reorder(messages, profile, historical)
	orderedCandidates := make([]candidate.Candidate, 0, len(order))
	orderedReports := make([]grounding.Result, 0, len(order))
	orderedAdjustments := make([]learning.Adjustment, 0, len(order))
	for rank, index := range order {
		value := candidates[index]
		value.Rank = rank
		orderedCandidates = append(orderedCandidates, value)
		orderedReports = append(orderedReports, reports[index])
		if len(explanation.Adjustments) == len(order) {
			adjustment := explanation.Adjustments[index]
			adjustment.Index = rank
			orderedAdjustments = append(orderedAdjustments, adjustment)
		}
	}
	if len(orderedAdjustments) == len(order) {
		explanation.Adjustments = orderedAdjustments
	}
	return orderedCandidates, orderedReports, explanation
}

func (server *Server) learningProfile(repository string) (learning.Profile, bool) {
	if server.learner == nil {
		return learning.Profile{}, false
	}
	server.learningMu.Lock()
	defer server.learningMu.Unlock()
	if profile, found := server.learningProfiles[repository]; found {
		return profile, true
	}
	profile, err := server.learner.Load(repository)
	if err != nil {
		return learning.Profile{}, false
	}
	server.setLearningProfileLocked(repository, profile)
	return profile, true
}

func (server *Server) compileContext(ctx context.Context, cwd string, state gitstate.State, signals []activity.Signal) (repoctx.Compiled, error) {
	server.providerMu.RLock()
	name := server.provider.Metadata().Name
	server.providerMu.RUnlock()
	return repoctx.CompileWithActivity(ctx, cwd, state, name, signals)
}

func (server *Server) generateCandidates(ctx context.Context, cwd string, expected gitstate.State, compiled repoctx.Compiled) (provider.Response, *CloudProvenance, error) {
	server.providerMu.RLock()
	selected, fallback := server.provider, server.fallback
	server.providerMu.RUnlock()
	request := provider.Request{CWD: cwd, Context: compiled.Prompt(), ContextFingerprint: compiled.ContextFingerprint}
	if provider.IsCloud(selected.Metadata().Name) {
		if server.cloud == nil {
			return provider.Response{}, nil, fmt.Errorf("cloud grant storage is unavailable")
		}
		grant, err := server.cloud.Grant(selected.Metadata().Name)
		if err != nil || len(grant.Classes) == 0 {
			return provider.Response{}, nil, fmt.Errorf("cloud context is not explicitly granted")
		}
		transmission, err := compiled.SelectCloud(selected.Metadata().Name, grant.Classes)
		if err != nil {
			return provider.Response{}, nil, err
		}
		if transmission.TotalBytes == 0 {
			return provider.Response{}, nil, fmt.Errorf("no selected cloud context is available for the granted classes")
		}
		provenance := &CloudProvenance{Provider: transmission.Provider, Classes: transmission.Classes, ContextFingerprint: transmission.ContextFingerprint}
		request.Context, request.ContextFingerprint = transmission.Prompt(), transmission.ContextFingerprint
		request.Authorized = func(context.Context) bool { return server.cloudAllowed(*provenance) }
		request.RetryAllowed = func(context.Context) bool { return server.cloudStateCurrent(ctx, cwd, expected) }
		if !request.Authorized(ctx) || !request.RetryAllowed(ctx) {
			return provider.Response{}, nil, fmt.Errorf("cloud transmission is no longer authorized for the staged state")
		}
		response, err := selected.Generate(ctx, request)
		return response, provenance, err
	}
	response, err := selected.Generate(ctx, request)
	if err == nil || fallback == nil {
		return response, nil, err
	}
	response, err = fallback.Generate(ctx, request)
	return response, nil, err
}

func (server *Server) generateFallback(ctx context.Context, cwd string, compiled repoctx.Compiled, selectedName string) (provider.Response, error) {
	if provider.IsCloud(selectedName) {
		return provider.Response{}, fmt.Errorf("cloud providers have no automatic fallback")
	}
	server.providerMu.RLock()
	fallback := server.fallback
	server.providerMu.RUnlock()
	if fallback == nil || fallback.Metadata().Name == selectedName {
		return provider.Response{}, fmt.Errorf("no distinct deterministic fallback is configured")
	}
	return fallback.Generate(ctx, provider.Request{CWD: cwd, Context: compiled.Prompt(), ContextFingerprint: compiled.ContextFingerprint})
}

func (server *Server) cloudAllowed(provenance CloudProvenance) bool {
	if server.cloud == nil || !cloud.ValidProvider(provenance.Provider) || len(provenance.Classes) == 0 || len(provenance.ContextFingerprint) != 64 {
		return false
	}
	grant, err := server.cloud.Grant(provenance.Provider)
	return err == nil && cloud.SameClasses(grant.Classes, provenance.Classes)
}

func (server *Server) cloudStateCurrent(ctx context.Context, cwd string, expected gitstate.State) bool {
	if ctx.Err() != nil {
		return false
	}
	checkContext, cancel := gitstate.WithTimeout()
	defer cancel()
	current, err := gitstate.Snapshot(checkContext, cwd)
	return err == nil && current.Availability == gitstate.Ready && current.Fingerprint == expected.Fingerprint
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
	server.activity.Configure(activitySettings(settings))
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
	if !server.matchesLearning(record) {
		server.discardRecord(record.Fingerprint, false)
		return ipc.Reply{Version: ipc.Version, Status: "stale"}
	}
	if record.Cloud != nil && !server.cloudAllowed(*record.Cloud) {
		server.discardRecord(record.Fingerprint, true)
		return ipc.Reply{Version: ipc.Version, Status: "stale"}
	}
	if !matchesActivity(record, server.activity.Provenance(request.Repository, request.Worktree)) {
		return ipc.Reply{Version: ipc.Version, Status: "activity_stale"}
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return ipc.Reply{Version: ipc.Version, Status: "error", Error: err.Error()}
	}
	return ipc.Reply{Version: ipc.Version, Status: "ready", Payload: payload}
}

func matchesActivity(record Record, current activity.Provenance) bool {
	return record.Activity.Fingerprint == current.Fingerprint
}

func (server *Server) matchesLearning(record Record) bool {
	if server.learner == nil {
		return true
	}
	profile, found := server.learningProfile(record.Repository)
	return !found || record.Learning.ProfileVersion == profile.Version
}

func (server *Server) ingestActivity(request ipc.Request) ipc.Reply {
	if request.Event == nil {
		return ipc.Reply{Version: ipc.Version, Status: "malformed", Error: "missing event"}
	}
	state, reply := activityScope(request.CWD)
	if reply != nil {
		return *reply
	}
	if request.Event.Repository != state.RepoID || request.Event.Worktree != state.WorktreeID {
		return ipc.Reply{Version: ipc.Version, Status: "rejected", Error: "event scope does not match working directory"}
	}
	decision := server.activity.Ingest(*request.Event)
	payload, _ := json.Marshal(decision)
	status := "rejected"
	if decision.Accepted {
		status = "accepted"
	}
	return ipc.Reply{Version: ipc.Version, Status: status, Payload: payload}
}

func (server *Server) inspectActivity(cwd string) ipc.Reply {
	state, reply := activityScope(cwd)
	if reply != nil {
		return *reply
	}
	payload, err := json.Marshal(server.activity.Inspect(state.RepoID, state.WorktreeID))
	if err != nil {
		return ipc.Reply{Version: ipc.Version, Status: "error", Error: err.Error()}
	}
	return ipc.Reply{Version: ipc.Version, Status: "ready", Payload: payload}
}

func (server *Server) clearActivity(cwd string) ipc.Reply {
	state, reply := activityScope(cwd)
	if reply != nil {
		return *reply
	}
	server.activity.Clear(state.RepoID, state.WorktreeID)
	return ipc.Reply{Version: ipc.Version, Status: "cleared"}
}

func (server *Server) observeActivityGit(cwd string, commit bool) ipc.Reply {
	if !server.activity.Enabled() {
		return ipc.Reply{Version: ipc.Version, Status: "rejected", Error: "activity permission is disabled"}
	}
	state, reply := activityScope(cwd)
	if reply != nil {
		return *reply
	}
	if state.Head == "" || state.IndexTree == "" {
		return ipc.Reply{Version: ipc.Version, Status: string(state.Availability), Error: "Git state is unavailable"}
	}
	payload, err := json.Marshal(server.activity.ObserveGit(state.RepoID, state.WorktreeID, state.Head, state.IndexTree, commit))
	if err != nil {
		return ipc.Reply{Version: ipc.Version, Status: "error", Error: err.Error()}
	}
	return ipc.Reply{Version: ipc.Version, Status: "ready", Payload: payload}
}

// prepareLearning stores a short-lived pre-commit candidate snapshot only in
// daemon memory. A failed or aborted commit therefore cannot create a durable
// learning signal, and generated messages are never written into profiles.
func (server *Server) prepareLearning(cwd string) ipc.Reply {
	if server.learner == nil {
		return ipc.Reply{Version: ipc.Version, Status: "ignored"}
	}
	state, reply := activityScope(cwd)
	if reply != nil {
		return *reply
	}
	profile, found := server.learningProfile(state.RepoID)
	if !found || !profile.Enabled {
		return ipc.Reply{Version: ipc.Version, Status: "ignored"}
	}
	prepared := preparedLearning{repository: state.RepoID, worktree: state.WorktreeID, head: state.Head, createdAt: time.Now()}
	if state.Availability == gitstate.Ready {
		lookup := server.lookup(ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID})
		if lookup.Status == "ready" {
			var record Record
			if json.Unmarshal(lookup.Payload, &record) == nil {
				prepared.candidates = make([]string, len(record.Candidates))
				for index, value := range record.Candidates {
					prepared.candidates[index] = value.Message
				}
			}
		}
	}
	server.learningMu.Lock()
	server.prunePreparedLocked(time.Now())
	if _, exists := server.prepared[state.WorktreeID]; !exists && len(server.prepared) >= server.settings.MaxRepositories {
		server.dropOldestPreparedLocked()
	}
	server.prepared[state.WorktreeID] = prepared
	server.learningMu.Unlock()
	return ipc.Reply{Version: ipc.Version, Status: "ready"}
}

func (server *Server) commitLearning(cwd string) ipc.Reply {
	if server.learner == nil {
		return ipc.Reply{Version: ipc.Version, Status: "ignored"}
	}
	state, reply := activityScope(cwd)
	if reply != nil {
		return *reply
	}
	server.learningMu.Lock()
	server.prunePreparedLocked(time.Now())
	prepared, found := server.prepared[state.WorktreeID]
	delete(server.prepared, state.WorktreeID)
	server.learningMu.Unlock()
	if !found || prepared.repository != state.RepoID || prepared.worktree != state.WorktreeID || prepared.head == state.Head {
		return ipc.Reply{Version: ipc.Version, Status: "ignored"}
	}
	profile, profileFound := server.learningProfile(state.RepoID)
	if !profileFound || !profile.Enabled {
		return ipc.Reply{Version: ipc.Version, Status: "ignored"}
	}
	metadataContext, cancel := gitstate.WithTimeout()
	subject, hasBody, err := learning.LatestCommit(metadataContext, cwd)
	cancel()
	if err != nil {
		return ipc.Reply{Version: ipc.Version, Status: "ignored"}
	}
	origin, rejectedVerb := classifyLearningCommit(subject, prepared.candidates)
	updated, err := server.learner.Observe(state.RepoID, learning.Observation{Subject: subject, HasBody: hasBody, Origin: origin, RejectedVerb: rejectedVerb})
	if err != nil {
		return ipc.Reply{Version: ipc.Version, Status: "error", Error: err.Error()}
	}
	server.learningMu.Lock()
	server.setLearningProfileLocked(state.RepoID, updated)
	server.learningMu.Unlock()
	server.invalidateLearningRepository(state.RepoID)
	return ipc.Reply{Version: ipc.Version, Status: "ready"}
}

func classifyLearningCommit(subject string, candidates []string) (learning.Origin, string) {
	for index, candidate := range candidates {
		if subject == candidate {
			if index == 0 {
				return learning.AcceptedPrimary, ""
			}
			return learning.AcceptedAlternative, ""
		}
	}
	if len(candidates) > 0 && learning.Related(subject, candidates[0]) {
		return learning.EditedCandidate, learning.GenericVerb(candidates[0])
	}
	return learning.UserAuthored, ""
}

func (server *Server) learningChanged(cwd string) ipc.Reply {
	if server.learner == nil {
		return ipc.Reply{Version: ipc.Version, Status: "ignored"}
	}
	state, reply := activityScope(cwd)
	if reply != nil {
		return *reply
	}
	profile, err := server.learner.Load(state.RepoID)
	if err != nil {
		return ipc.Reply{Version: ipc.Version, Status: "error", Error: err.Error()}
	}
	server.learningMu.Lock()
	server.setLearningProfileLocked(state.RepoID, profile)
	server.learningMu.Unlock()
	server.invalidateLearningRepository(state.RepoID)
	return ipc.Reply{Version: ipc.Version, Status: "ready"}
}

// cloudChanged removes only records derived from a changed provider grant.
// lookup independently reloads grants, so a missed notification also fails
// closed before a cloud-derived suggestion can render.
func (server *Server) cloudChanged(name string) ipc.Reply {
	if !cloud.ValidProvider(name) {
		return ipc.Reply{Version: ipc.Version, Status: "malformed", Error: "unsupported cloud provider"}
	}
	server.mu.Lock()
	// A grant replacement or revocation must also stop an in-flight HTTP
	// attempt. Jobs are few and bounded; cancelling a concurrent local job only
	// defers its optional background preparation to the next observation.
	for _, pending := range server.jobs {
		pending.cancel()
	}
	for fingerprint, record := range server.cache {
		if record.Cloud != nil && record.Cloud.Provider == name {
			delete(server.cache, fingerprint)
		}
	}
	server.mu.Unlock()
	server.cacheMu.Lock()
	entries, err := os.ReadDir(server.cacheDir)
	if err == nil {
		for _, entry := range entries {
			fingerprint, valid := cacheFilename(entry.Name())
			if !valid {
				continue
			}
			content, readErr := os.ReadFile(server.cachePath(fingerprint))
			var record Record
			if readErr == nil && json.Unmarshal(content, &record) == nil && record.Cloud != nil && record.Cloud.Provider == name {
				server.removeCachePathLocked(fingerprint, &server.cacheStatus.StaleDiscarded)
			}
		}
	}
	server.cacheMu.Unlock()
	return ipc.Reply{Version: ipc.Version, Status: "ready"}
}

func (server *Server) prunePreparedLocked(now time.Time) {
	for worktree, prepared := range server.prepared {
		if now.Sub(prepared.createdAt) > 10*time.Minute {
			delete(server.prepared, worktree)
		}
	}
}

func (server *Server) dropOldestPreparedLocked() {
	var oldest string
	for worktree, prepared := range server.prepared {
		if oldest == "" || prepared.createdAt.Before(server.prepared[oldest].createdAt) {
			oldest = worktree
		}
	}
	if oldest != "" {
		delete(server.prepared, oldest)
	}
}

func (server *Server) setLearningProfileLocked(repository string, profile learning.Profile) {
	if _, exists := server.learningProfiles[repository]; !exists && len(server.learningProfiles) >= server.settings.MaxRepositories {
		for stale := range server.learningProfiles {
			delete(server.learningProfiles, stale)
			break
		}
	}
	server.learningProfiles[repository] = profile
}

func (server *Server) invalidateLearningRepository(repository string) {
	server.mu.Lock()
	for fingerprint, record := range server.cache {
		if record.Repository == repository {
			delete(server.cache, fingerprint)
		}
	}
	server.mu.Unlock()
	server.cacheMu.Lock()
	entries, err := os.ReadDir(server.cacheDir)
	if err == nil {
		for _, entry := range entries {
			fingerprint, valid := cacheFilename(entry.Name())
			if !valid {
				continue
			}
			content, readErr := os.ReadFile(server.cachePath(fingerprint))
			var record Record
			if readErr == nil && json.Unmarshal(content, &record) == nil && record.Repository == repository {
				server.removeCachePathLocked(fingerprint, &server.cacheStatus.StaleDiscarded)
			}
		}
	}
	server.cacheMu.Unlock()
}

func activityScope(cwd string) (gitstate.State, *ipc.Reply) {
	if cwd == "" || len(cwd) > 4096 {
		reply := ipc.Reply{Version: ipc.Version, Status: "malformed", Error: "invalid working directory"}
		return gitstate.State{}, &reply
	}
	context, cancel := gitstate.WithTimeout()
	defer cancel()
	state, err := gitstate.Snapshot(context, cwd)
	if err != nil {
		reply := ipc.Reply{Version: ipc.Version, Status: "error", Error: err.Error()}
		return gitstate.State{}, &reply
	}
	if state.Root == "" || state.RepoID == "" || state.WorktreeID == "" {
		reply := ipc.Reply{Version: ipc.Version, Status: string(state.Availability), Error: state.Reason}
		return gitstate.State{}, &reply
	}
	return state, nil
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
	if record.Fingerprint != fingerprint || record.Repository == "" || record.Worktree == "" || len(record.ContextFingerprint) != 64 || record.CreatedAt.IsZero() || !record.Provider.Valid() || len(record.Candidates) == 0 || len(record.Candidates) > candidate.MaxCandidates || len(record.Grounding) != len(record.Candidates) || !learning.ValidExplanation(record.Learning, len(record.Candidates)) {
		return false
	}
	if record.Cloud != nil && (!provider.IsCloud(record.Provider.Name) || !cloud.ValidProvider(record.Cloud.Provider) || record.Cloud.Provider != record.Provider.Name || len(record.Cloud.ContextFingerprint) != 64 || !cloud.SameClasses(record.Cloud.Classes, record.Cloud.Classes)) {
		return false
	}
	if record.Cloud == nil && provider.IsCloud(record.Provider.Name) {
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
