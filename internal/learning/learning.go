// Package learning owns private, bounded, repository-local style preferences.
package learning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	SchemaVersion = 1
	maxProfiles   = 128
	maxProfile    = 16 * 1024
	maxSamples    = 4096
	maxTypes      = 16
	maxScopes     = 32
	maxVerbs      = 16
)

type Origin string

const (
	AcceptedPrimary     Origin = "accepted_primary"
	AcceptedAlternative Origin = "accepted_alternative"
	EditedCandidate     Origin = "edited_candidate"
	UserAuthored        Origin = "user_authored"
)

// Profile contains aggregate style preferences only. It deliberately has no
// commit subjects, source text, activity, provider state, or remote URL.
type Profile struct {
	Schema     int       `json:"schema_version"`
	Repository string    `json:"repository_id"`
	Version    uint64    `json:"version"`
	Enabled    bool      `json:"enabled"`
	UpdatedAt  time.Time `json:"updated_at"`
	Local      Stats     `json:"local_interactions"`
}

// Export is repository-independent so an explicit import cannot overwrite the
// target repository identity or transfer unrelated runtime state.
type Export struct {
	Schema  int   `json:"schema_version"`
	Enabled bool  `json:"enabled"`
	Local   Stats `json:"local_interactions"`
}

// Stats records bounded, interpretable style facts. All keys are parsed from
// constrained commit-subject components, not retained subject text.
type Stats struct {
	Samples        int            `json:"samples"`
	MeanLength     int            `json:"mean_subject_length"`
	Conventional   int            `json:"conventional_count"`
	Lowercase      int            `json:"lowercase_subject_count"`
	Imperative     int            `json:"imperative_verb_count"`
	SubjectOnly    int            `json:"subject_only_count"`
	Types          map[string]int `json:"types,omitempty"`
	Scopes         map[string]int `json:"scopes,omitempty"`
	PreferredVerbs map[string]int `json:"preferred_generic_verbs,omitempty"`
	AvoidedVerbs   map[string]int `json:"avoided_generic_verbs,omitempty"`
}

type Observation struct {
	Subject      string
	HasBody      bool
	Origin       Origin
	RejectedVerb string
}

type Adjustment struct {
	Index   int      `json:"index"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons,omitempty"`
}

// Explanation is stored beside a candidate record and makes an applied order
// inspectable without retaining either historical or final commit subjects.
type Explanation struct {
	Enabled           bool         `json:"enabled"`
	ProfileVersion    uint64       `json:"profile_version"`
	LocalSamples      int          `json:"local_samples"`
	HistoricalSamples int          `json:"historical_samples"`
	Adjustments       []Adjustment `json:"adjustments,omitempty"`
}

type Store struct {
	directory string
	now       func() time.Time
}

func New(directory string) (*Store, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create learning directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	stat, owned := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || !owned || int(stat.Uid) != os.Getuid() {
		return nil, fmt.Errorf("learning directory is not private")
	}
	return &Store{directory: directory, now: time.Now}, nil
}

func (store *Store) Load(repository string) (Profile, error) {
	if !validRepository(repository) {
		return Profile{}, fmt.Errorf("invalid repository identity")
	}
	path := store.path(repository)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return store.defaultProfile(repository), nil
	}
	if err != nil {
		return Profile{}, err
	}
	stat, owned := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() > maxProfile || !owned || int(stat.Uid) != os.Getuid() {
		return Profile{}, fmt.Errorf("learning profile is not private")
	}
	file, err := os.Open(path)
	if err != nil {
		return Profile{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxProfile))
	decoder.DisallowUnknownFields()
	var profile Profile
	if err := decoder.Decode(&profile); err != nil {
		return Profile{}, fmt.Errorf("decode learning profile: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Profile{}, fmt.Errorf("decode learning profile: trailing content")
	}
	if err := validProfile(profile, repository); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func (store *Store) Observe(repository string, observation Observation) (Profile, error) {
	profile, err := store.Load(repository)
	if err != nil || !profile.Enabled {
		return profile, err
	}
	if observation.Origin != AcceptedPrimary && observation.Origin != AcceptedAlternative && observation.Origin != EditedCandidate && observation.Origin != UserAuthored {
		return Profile{}, fmt.Errorf("invalid learning observation origin")
	}
	if len(observation.Subject) == 0 || len(observation.Subject) > 200 || strings.ContainsAny(observation.Subject, "\x00\r\n") {
		return profile, nil
	}
	add(&profile.Local, observation)
	profile.Version++
	profile.UpdatedAt = store.now().UTC()
	if err := store.save(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func (store *Store) SetEnabled(repository string, enabled bool) (Profile, error) {
	profile, err := store.Load(repository)
	if err != nil {
		return Profile{}, err
	}
	profile.Enabled = enabled
	profile.Version++
	profile.UpdatedAt = store.now().UTC()
	if err := store.save(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func (store *Store) Reset(repository string) error {
	if !validRepository(repository) {
		return fmt.Errorf("invalid repository identity")
	}
	path := store.path(repository)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("learning profile is not a regular file")
	}
	return os.Remove(path)
}

func (store *Store) Export(repository string) (Export, error) {
	profile, err := store.Load(repository)
	if err != nil {
		return Export{}, err
	}
	return Export{Schema: SchemaVersion, Enabled: profile.Enabled, Local: profile.Local}, nil
}

func (store *Store) Import(repository string, exported Export) (Profile, error) {
	if exported.Schema != SchemaVersion {
		return Profile{}, fmt.Errorf("unsupported learning export schema")
	}
	profile := store.defaultProfile(repository)
	profile.Enabled = exported.Enabled
	profile.Local = exported.Local
	if err := validStats(profile.Local); err != nil {
		return Profile{}, err
	}
	previous, err := store.Load(repository)
	if err != nil {
		return Profile{}, err
	}
	profile.Version = previous.Version + 1
	profile.UpdatedAt = store.now().UTC()
	if err := store.save(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func (store *Store) path(repository string) string {
	return filepath.Join(store.directory, repository+".json")
}

func (store *Store) defaultProfile(repository string) Profile {
	return Profile{Schema: SchemaVersion, Repository: repository, Version: 1, Enabled: true, UpdatedAt: store.now().UTC(), Local: emptyStats()}
}

func (store *Store) save(profile Profile) error {
	if err := validProfile(profile, profile.Repository); err != nil {
		return err
	}
	content, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	if len(content) > maxProfile {
		return fmt.Errorf("learning profile exceeds %d bytes", maxProfile)
	}
	temporary, err := os.CreateTemp(store.directory, ".learning-")
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
	if err := os.Rename(name, store.path(profile.Repository)); err != nil {
		return err
	}
	return store.prune(profile.Repository)
}

func (store *Store) prune(keep string) error {
	entries, err := os.ReadDir(store.directory)
	if err != nil {
		return err
	}
	type entry struct {
		name    string
		updated time.Time
	}
	profiles := make([]entry, 0, len(entries))
	for _, value := range entries {
		name := value.Name()
		if !strings.HasSuffix(name, ".json") || !validRepository(strings.TrimSuffix(name, ".json")) {
			continue
		}
		profile, loadErr := store.Load(strings.TrimSuffix(name, ".json"))
		if loadErr != nil {
			continue
		}
		profiles = append(profiles, entry{name: name, updated: profile.UpdatedAt})
	}
	if len(profiles) <= maxProfiles {
		return nil
	}
	sort.Slice(profiles, func(left, right int) bool {
		if profiles[left].updated.Equal(profiles[right].updated) {
			return profiles[left].name < profiles[right].name
		}
		return profiles[left].updated.Before(profiles[right].updated)
	})
	remaining := len(profiles) - maxProfiles
	for _, profile := range profiles {
		if remaining == 0 {
			break
		}
		if profile.name == keep+".json" {
			continue
		}
		if err := os.Remove(filepath.Join(store.directory, profile.name)); err != nil {
			return err
		}
		remaining--
	}
	return nil
}

func Reorder(messages []string, profile Profile, historical Stats) ([]int, Explanation) {
	explanation := Explanation{Enabled: profile.Enabled, ProfileVersion: profile.Version, LocalSamples: profile.Local.Samples, HistoricalSamples: historical.Samples}
	order := make([]int, len(messages))
	adjustments := make([]Adjustment, len(messages))
	for index, message := range messages {
		order[index] = index
		adjustments[index] = score(index, message, profile.Local, historical, profile.Enabled)
	}
	sort.SliceStable(order, func(left, right int) bool {
		return adjustments[order[left]].Score > adjustments[order[right]].Score
	})
	if profile.Enabled {
		explanation.Adjustments = adjustments
	}
	return order, explanation
}

func score(index int, message string, local, historical Stats, enabled bool) Adjustment {
	adjustment := Adjustment{Index: index}
	if !enabled {
		return adjustment
	}
	parsed := parse(message)
	localScore, localReasons := scoreStats(parsed, local, 2, "local")
	historyScore, historyReasons := scoreStats(parsed, historical, 1, "historical")
	adjustment.Score = clamp(localScore, -6, 6) + clamp(historyScore, -3, 3)
	adjustment.Reasons = append(localReasons, historyReasons...)
	return adjustment
}

func scoreStats(parsed subject, stats Stats, weight int, source string) (int, []string) {
	if stats.Samples == 0 {
		return 0, nil
	}
	score, reasons := 0, []string{}
	if parsed.typ != "" && stats.Types[parsed.typ] > 0 {
		score += weight
		reasons = append(reasons, source+"_type="+parsed.typ)
	}
	if parsed.scope != "" && stats.Scopes[parsed.scope] > 0 {
		score += weight
		reasons = append(reasons, source+"_scope="+parsed.scope)
	}
	if parsed.verb != "" && stats.PreferredVerbs[parsed.verb] > 0 {
		score += weight
		reasons = append(reasons, source+"_preferred_verb="+parsed.verb)
	}
	if parsed.verb != "" && stats.AvoidedVerbs[parsed.verb] > 0 {
		score -= weight
		reasons = append(reasons, source+"_avoided_verb="+parsed.verb)
	}
	if stats.MeanLength > 0 && abs(parsed.length-stats.MeanLength) <= 12 {
		score += weight
		reasons = append(reasons, source+"_subject_length")
	}
	return score, reasons
}

type subject struct {
	typ        string
	scope      string
	verb       string
	length     int
	lowercase  bool
	imperative bool
}

var (
	conventional = regexp.MustCompile(`^([a-z][a-z0-9-]{0,31})(?:\(([a-z][a-z0-9._/-]{0,39})\))?!?:[ ]+(.+)$`)
	firstWord    = regexp.MustCompile(`^[a-z][a-z-]{0,31}`)
	identifier   = regexp.MustCompile(`^[a-z][a-z0-9._/-]{0,39}$`)
	reason       = regexp.MustCompile(`^[a-z0-9_./=-]{1,80}$`)
	genericVerb  = map[string]bool{"add": true, "change": true, "fix": true, "improve": true, "refine": true, "remove": true, "update": true}
)

func add(stats *Stats, observation Observation) {
	ensureStats(stats)
	if stats.Samples >= maxSamples {
		decay(stats)
	}
	parsed := parse(observation.Subject)
	stats.MeanLength = (stats.MeanLength*stats.Samples + parsed.length) / (stats.Samples + 1)
	stats.Samples++
	if parsed.typ != "" {
		stats.Conventional++
		increment(stats.Types, parsed.typ, maxTypes)
	}
	if parsed.scope != "" {
		increment(stats.Scopes, parsed.scope, maxScopes)
	}
	if parsed.lowercase {
		stats.Lowercase++
	}
	if parsed.imperative {
		stats.Imperative++
	}
	if !observation.HasBody {
		stats.SubjectOnly++
	}
	if parsed.verb != "" && genericVerb[parsed.verb] {
		increment(stats.PreferredVerbs, parsed.verb, maxVerbs)
	}
	if genericVerb[observation.RejectedVerb] {
		increment(stats.AvoidedVerbs, observation.RejectedVerb, maxVerbs)
	}
}

func parse(value string) subject {
	parsed := subject{}
	match := conventional.FindStringSubmatch(value)
	subjectText := value
	if len(match) != 0 {
		parsed.typ, parsed.scope, subjectText = match[1], match[2], match[3]
	}
	parsed.length = len(subjectText)
	parsed.lowercase = len(subjectText) > 0 && subjectText[0] >= 'a' && subjectText[0] <= 'z'
	if word := firstWord.FindString(subjectText); genericVerb[word] {
		parsed.verb = word
		parsed.imperative = true
	}
	return parsed
}

func increment(values map[string]int, key string, maximum int) {
	if key == "" {
		return
	}
	values[key]++
	trim(values, maximum)
}

func trim(values map[string]int, maximum int) {
	if len(values) <= maximum {
		return
	}
	type pair struct {
		key   string
		value int
	}
	pairs := make([]pair, 0, len(values))
	for key, value := range values {
		pairs = append(pairs, pair{key, value})
	}
	sort.Slice(pairs, func(left, right int) bool {
		if pairs[left].value == pairs[right].value {
			return pairs[left].key < pairs[right].key
		}
		return pairs[left].value > pairs[right].value
	})
	for _, pair := range pairs[maximum:] {
		delete(values, pair.key)
	}
}

func decay(stats *Stats) {
	stats.Samples = (stats.Samples + 1) / 2
	stats.Conventional = (stats.Conventional + 1) / 2
	stats.Lowercase = (stats.Lowercase + 1) / 2
	stats.Imperative = (stats.Imperative + 1) / 2
	stats.SubjectOnly = (stats.SubjectOnly + 1) / 2
	for _, values := range []map[string]int{stats.Types, stats.Scopes, stats.PreferredVerbs, stats.AvoidedVerbs} {
		for key, value := range values {
			if value <= 1 {
				delete(values, key)
			} else {
				values[key] = (value + 1) / 2
			}
		}
	}
}

func emptyStats() Stats {
	return Stats{Types: map[string]int{}, Scopes: map[string]int{}, PreferredVerbs: map[string]int{}, AvoidedVerbs: map[string]int{}}
}

func ensureStats(stats *Stats) {
	if stats.Types == nil {
		stats.Types = map[string]int{}
	}
	if stats.Scopes == nil {
		stats.Scopes = map[string]int{}
	}
	if stats.PreferredVerbs == nil {
		stats.PreferredVerbs = map[string]int{}
	}
	if stats.AvoidedVerbs == nil {
		stats.AvoidedVerbs = map[string]int{}
	}
}

func validProfile(profile Profile, repository string) error {
	if profile.Schema != SchemaVersion || profile.Repository != repository || !validRepository(repository) || profile.Version == 0 || profile.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid learning profile")
	}
	return validStats(profile.Local)
}

func validStats(stats Stats) error {
	if stats.Samples < 0 || stats.Samples > maxSamples || stats.MeanLength < 0 || stats.MeanLength > 200 || stats.Conventional < 0 || stats.Conventional > stats.Samples || stats.Lowercase < 0 || stats.Lowercase > stats.Samples || stats.Imperative < 0 || stats.Imperative > stats.Samples || stats.SubjectOnly < 0 || stats.SubjectOnly > stats.Samples {
		return fmt.Errorf("invalid learning statistics")
	}
	for _, value := range []struct {
		values  map[string]int
		maximum int
	}{
		{stats.Types, maxTypes},
		{stats.Scopes, maxScopes},
		{stats.PreferredVerbs, maxVerbs},
		{stats.AvoidedVerbs, maxVerbs},
	} {
		if len(value.values) > value.maximum {
			return fmt.Errorf("learning statistics exceed bounds")
		}
		for key, count := range value.values {
			if !identifier.MatchString(key) || count < 1 || count > maxSamples {
				return fmt.Errorf("invalid learning statistic")
			}
		}
	}
	return nil
}

func validRepository(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func NormalizeRemote(value string) string {
	value = strings.TrimSpace(value)
	value = strings.SplitN(value, "#", 2)[0]
	value = strings.SplitN(value, "?", 2)[0]
	if at := strings.LastIndex(value, "@"); at >= 0 && !strings.Contains(value[:at], "://") {
		value = value[at+1:]
	}
	if scheme := strings.Index(value, "://"); scheme >= 0 {
		prefix, rest := strings.ToLower(value[:scheme]), value[scheme+3:]
		if at := strings.LastIndex(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		value = prefix + "://" + strings.ToLower(rest)
	} else {
		value = strings.ToLower(value)
	}
	value = strings.TrimSuffix(strings.TrimSuffix(value, "/"), ".git")
	return value
}

func RemoteDigest(value string) string {
	sum := sha256.Sum256([]byte(NormalizeRemote(value)))
	return hex.EncodeToString(sum[:])
}

// GenericVerb returns an allowlisted generic imperative verb, if present. It
// is safe to retain as a bounded preference category rather than source text.
func GenericVerb(message string) string { return parse(message).verb }

// Related reports a conservative style overlap used only to label a weak
// edited-candidate observation after a real commit. It does not compare or
// retain arbitrary prose.
func Related(left, right string) bool {
	first, second := parse(left), parse(right)
	return first.typ != "" && first.typ == second.typ && first.scope == second.scope
}

// ValidExplanation rejects malformed cached ranking diagnostics before they
// can be shown by explain. It permits the zero value for pre-learning records.
func ValidExplanation(value Explanation, candidates int) bool {
	if !value.Enabled {
		return len(value.Adjustments) == 0
	}
	if value.ProfileVersion == 0 || value.LocalSamples < 0 || value.LocalSamples > maxSamples || value.HistoricalSamples < 0 || value.HistoricalSamples > maxSamples || len(value.Adjustments) != candidates {
		return false
	}
	for index, adjustment := range value.Adjustments {
		if adjustment.Index != index || adjustment.Score < -9 || adjustment.Score > 9 || len(adjustment.Reasons) > 8 {
			return false
		}
		for _, item := range adjustment.Reasons {
			if !reason.MatchString(item) {
				return false
			}
		}
	}
	return true
}

func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
