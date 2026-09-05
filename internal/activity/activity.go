// Package activity owns consented, bounded, repository-scoped local events.
package activity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	SchemaVersion = 1
	maxDataFields = 12
	maxDataBytes  = 256
)

type Sensitivity string

const (
	Public  Sensitivity = "public"
	Private Sensitivity = "private"
	Secret  Sensitivity = "secret"
)

// Event is the versioned, bounded wire representation accepted from local
// producers. It contains no executable or provider-routing fields.
type Event struct {
	Schema      int               `json:"schema_version"`
	Repository  string            `json:"repository_id"`
	Worktree    string            `json:"worktree_id"`
	Source      string            `json:"source"`
	Kind        string            `json:"kind"`
	Timestamp   time.Time         `json:"timestamp"`
	Data        map[string]string `json:"data,omitempty"`
	Sensitivity Sensitivity       `json:"sensitivity"`
}

// UnmarshalJSON makes the producer schema strict even though other IPC
// requests intentionally remain forwards-compatible.
func (event *Event) UnmarshalJSON(content []byte) error {
	type wire Event
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var decoded wire
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("invalid event JSON")
	}
	*event = Event(decoded)
	return nil
}

type Permissions struct {
	Activity bool `json:"activity"`
}

type Settings struct {
	Retention time.Duration
	MaxEvents int
	MaxScopes int
}

func DefaultSettings() Settings {
	return Settings{Retention: 30 * time.Minute, MaxEvents: 256, MaxScopes: 32}
}

type Decision struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason"`
}

// Inspection exposes only already-redacted selected events and aggregate
// rejection reasons. Invalid producer payloads are never retained for review.
type Inspection struct {
	Enabled  bool           `json:"enabled"`
	Selected []Event        `json:"selected"`
	Excluded map[string]int `json:"excluded"`
}

// Signal is safe for provider context: it contains an allowlisted event kind
// and a bounded count, never producer data.
type Signal struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// Provenance allows a candidate record to be rejected when its derived
// activity signal set changes or expires. It intentionally contains no event
// data or timestamps.
type Provenance struct {
	Fingerprint string   `json:"fingerprint,omitempty"`
	Signals     []Signal `json:"signals,omitempty"`
}

type scopedEvents struct {
	events   []Event
	excluded map[string]int
	seen     time.Time
}

type Store struct {
	mu         sync.Mutex
	settings   Settings
	permission func() (Permissions, error)
	scopes     map[string]scopedEvents
	now        func() time.Time
}

func New(settings Settings, permission func() (Permissions, error)) *Store {
	return &Store{
		settings:   normalize(settings),
		permission: permission,
		scopes:     map[string]scopedEvents{},
		now:        time.Now,
	}
}

// Configure changes only retention bounds; it never grants collection.
func (store *Store) Configure(settings Settings) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.settings = normalize(settings)
	store.pruneLocked(store.now())
	for key, value := range store.scopes {
		if len(value.events) > store.settings.MaxEvents {
			value.events = append([]Event(nil), value.events[len(value.events)-store.settings.MaxEvents:]...)
			store.scopes[key] = value
		}
	}
}

// Ingest checks the durable user grant for every producer event, so a revoke
// affects an already-running daemon without a restart.
func (store *Store) Ingest(event Event) Decision {
	if !store.enabled() {
		return Decision{Reason: "activity permission is disabled"}
	}
	if err := validIdentity(event); err != nil {
		return Decision{Reason: "invalid event identity"}
	}
	if err := valid(event, store.now()); err != nil {
		store.noteExcluded(scope(event.Repository, event.Worktree), "invalid event")
		return Decision{Reason: "invalid event"}
	}
	event.Timestamp = event.Timestamp.UTC()
	event.Data = redact(event)
	key := scope(event.Repository, event.Worktree)
	store.mu.Lock()
	defer store.mu.Unlock()
	now := store.now()
	store.pruneLocked(now)
	if event.Timestamp.Before(now.Add(-store.settings.Retention)) {
		store.noteExcludedLocked(key, "expired event")
		return Decision{Reason: "expired event"}
	}
	value := store.scopeLocked(key, now)
	value.events = append(value.events, event)
	if len(value.events) > store.settings.MaxEvents {
		value.events = append([]Event(nil), value.events[len(value.events)-store.settings.MaxEvents:]...)
	}
	value.seen = now
	store.scopes[key] = value
	return Decision{Accepted: true, Reason: "accepted"}
}

func (store *Store) Inspect(repository, worktree string) Inspection {
	if !store.enabled() {
		return Inspection{Selected: []Event{}, Excluded: map[string]int{}}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.pruneLocked(store.now())
	value := store.scopes[scope(repository, worktree)]
	return Inspection{Enabled: true, Selected: copyEvents(value.events), Excluded: copyCounts(value.excluded)}
}

func (store *Store) Clear(repository, worktree string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.scopes, scope(repository, worktree))
}

// Provenance returns a stable, bounded representation for provider context.
// It rechecks permission and prunes expiry before every result.
func (store *Store) Provenance(repository, worktree string) Provenance {
	if !store.enabled() {
		return Provenance{}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.pruneLocked(store.now())
	counts := map[string]int{}
	for _, event := range store.scopes[scope(repository, worktree)].events {
		counts[event.Kind]++
	}
	signals := make([]Signal, 0, len(counts))
	for _, kind := range orderedKinds {
		if counts[kind] > 0 {
			signals = append(signals, Signal{Kind: kind, Count: counts[kind]})
		}
	}
	if len(signals) == 0 {
		return Provenance{}
	}
	encoded, _ := json.Marshal(signals)
	return Provenance{Fingerprint: Digest(string(encoded)), Signals: signals}
}

func (store *Store) enabled() bool {
	if store.permission == nil {
		return false
	}
	permissions, err := store.permission()
	if err == nil && permissions.Activity {
		return true
	}
	store.mu.Lock()
	store.scopes = map[string]scopedEvents{}
	store.mu.Unlock()
	return false
}

func (store *Store) noteExcluded(key, reason string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.noteExcludedLocked(key, reason)
}

func (store *Store) noteExcludedLocked(key, reason string) {
	value := store.scopeLocked(key, store.now())
	value.excluded[reason]++
	store.scopes[key] = value
}

func (store *Store) scopeLocked(key string, now time.Time) scopedEvents {
	if value, found := store.scopes[key]; found {
		return value
	}
	if len(store.scopes) >= store.settings.MaxScopes {
		oldestKey := ""
		var oldest time.Time
		for candidate, value := range store.scopes {
			if oldestKey == "" || value.seen.Before(oldest) {
				oldestKey, oldest = candidate, value.seen
			}
		}
		delete(store.scopes, oldestKey)
	}
	return scopedEvents{excluded: map[string]int{}, seen: now}
}

func (store *Store) pruneLocked(now time.Time) {
	cutoff := now.Add(-store.settings.Retention)
	for key, value := range store.scopes {
		kept := value.events[:0]
		for _, event := range value.events {
			if !event.Timestamp.Before(cutoff) {
				kept = append(kept, event)
			}
		}
		value.events = kept
		if len(value.events) == 0 && len(value.excluded) == 0 {
			delete(store.scopes, key)
			continue
		}
		store.scopes[key] = value
	}
}

func normalize(settings Settings) Settings {
	defaults := DefaultSettings()
	if settings.Retention < time.Minute || settings.Retention > 24*time.Hour {
		settings.Retention = defaults.Retention
	}
	if settings.MaxEvents < 1 || settings.MaxEvents > 4096 {
		settings.MaxEvents = defaults.MaxEvents
	}
	if settings.MaxScopes < 1 || settings.MaxScopes > 256 {
		settings.MaxScopes = defaults.MaxScopes
	}
	return settings
}

func validIdentity(event Event) error {
	if len(event.Repository) != 64 || len(event.Worktree) != 64 || !hexID(event.Repository) || !hexID(event.Worktree) {
		return fmt.Errorf("invalid event identity")
	}
	return nil
}

func valid(event Event, now time.Time) error {
	if event.Schema != SchemaVersion || !allowedSources[event.Source] || !allowedKinds[event.Kind] || event.Timestamp.IsZero() || event.Timestamp.Before(now.Add(-24*time.Hour)) || event.Timestamp.After(now.Add(5*time.Minute)) {
		return fmt.Errorf("invalid event metadata")
	}
	if (event.Sensitivity != Public && event.Sensitivity != Private && event.Sensitivity != Secret) || len(event.Data) > maxDataFields {
		return fmt.Errorf("invalid event data")
	}
	for key, value := range event.Data {
		if !safeKey.MatchString(key) || len(value) > maxDataBytes || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("invalid event data")
		}
	}
	return nil
}

var (
	allowedSources = map[string]bool{"shell": true, "editor": true, "integration": true}
	orderedKinds   = []string{
		"shell.command_started", "shell.command_finished", "git.index_changed", "git.head_changed", "git.commit_completed",
		"editor.file_opened", "editor.file_saved", "lsp.diagnostics_changed", "build.completed", "test.completed",
	}
	allowedKinds = func() map[string]bool {
		values := make(map[string]bool, len(orderedKinds))
		for _, kind := range orderedKinds {
			values[kind] = true
		}
		return values
	}()
	safeKey   = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,39}$`)
	secretKey = regexp.MustCompile(`(?i)(password|secret|token|api[_-]?key|credential)`)
	secret    = regexp.MustCompile(`(?i)(password|secret|token|api[_-]?key)\s*[:=]\s*[^\s]+|\b(?:AKIA|ASIA)[A-Z0-9]{0,16}\b|\bgh[pousr]_[A-Za-z0-9]*\b`)
)

func redact(event Event) map[string]string {
	if len(event.Data) == 0 || event.Sensitivity == Secret {
		return nil
	}
	result := make(map[string]string, len(event.Data))
	for key, value := range event.Data {
		if secretKey.MatchString(key) || secret.MatchString(value) {
			result[key] = "[REDACTED]"
			continue
		}
		result[key] = value
	}
	return result
}

func ValidKind(value string) bool { return allowedKinds[value] }

func scope(repository, worktree string) string { return repository + ":" + worktree }

func hexID(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func copyEvents(values []Event) []Event {
	result := make([]Event, len(values))
	for index, event := range values {
		result[index] = event
		if event.Data != nil {
			result[index].Data = make(map[string]string, len(event.Data))
			for key, value := range event.Data {
				result[index].Data[key] = value
			}
		}
	}
	return result
}

func copyCounts(values map[string]int) map[string]int {
	result := make(map[string]int, len(values))
	for key, count := range values {
		result[key] = count
	}
	return result
}

func PermissionsPath(dataDir string) string { return filepath.Join(dataDir, "permissions.json") }

func LoadPermissions(path string) (Permissions, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Permissions{}, nil
	}
	if err != nil {
		return Permissions{}, err
	}
	stat, owned := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() > 1024 || !owned || int(stat.Uid) != os.Getuid() {
		return Permissions{}, fmt.Errorf("permission record is not private")
	}
	file, err := os.Open(path)
	if err != nil {
		return Permissions{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1024))
	decoder.DisallowUnknownFields()
	var permissions Permissions
	if err := decoder.Decode(&permissions); err != nil {
		return Permissions{}, fmt.Errorf("invalid permission record")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Permissions{}, fmt.Errorf("invalid permission record")
	}
	return permissions, nil
}

func SavePermissions(path string, permissions Permissions) error {
	content, err := json.Marshal(permissions)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".permissions-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(content)
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
	return os.Rename(name, path)
}

func Digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
