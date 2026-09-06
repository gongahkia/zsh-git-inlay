// Package evaluation runs reproducible candidate evaluations away from ZLE.
package evaluation

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/candidate"
	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/daemon"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/grounding"
	"github.com/gongahkia/zsh-git-inlay/internal/provider"
	"github.com/gongahkia/zsh-git-inlay/internal/repoctx"
)

const (
	SchemaVersion  = "v1"
	CorpusVersion  = "v2"
	DevelopmentSet = "development"
	HeldOutSet     = "held_out"
)

//go:embed testdata/corpus-v1.json
var embeddedCorpus []byte

var conventionalSubject = regexp.MustCompile(`^(feat|fix|docs|test|refactor|build|chore|ci|perf|style|revert)(\(([a-z0-9._/-]+)\))?!?: .+$`)
var issueIdentifier = regexp.MustCompile(`(?:#[0-9]+|\b[A-Z][A-Z0-9]+-[0-9]+\b)`)
var behavioralClaim = regexp.MustCompile(`\b(fix|prevent|ensure|avoid|resolve|eliminate)\b`)
var testOutcomeClaim = regexp.MustCompile(`\b(?:test|tests|test suite)\s+(?:pass|passes|passed|succeed|succeeds|succeeded)\b`)

// Generator produces candidates outside the interactive path. It deliberately
// has no capability to write Git state or publish daemon records.
type Generator interface {
	Metadata() ProviderMetadata
	Generate(context.Context, string) ([]candidate.Candidate, error)
}

// DetailedGenerator exposes publication evidence when an evaluator executes
// the complete daemon pipeline rather than a provider in isolation.
type DetailedGenerator interface {
	Generator
	GenerateDetailed(context.Context, string) (Generated, error)
}

// PreflightGenerator records bounded local runtime/model identity before any
// fixture is generated. It has no cloud behavior and does not pull a model.
type PreflightGenerator interface {
	Preflight(context.Context) error
}

type Generated struct {
	Candidates []candidate.Candidate
	Grounding  []grounding.Result
}

type ProviderMetadata struct {
	Name          string            `json:"name"`
	Model         string            `json:"model"`
	ModelDigest   string            `json:"model_digest,omitempty"`
	ModelSize     int64             `json:"model_size_bytes,omitempty"`
	Quantization  string            `json:"quantization,omitempty"`
	Runtime       string            `json:"runtime"`
	PromptVersion string            `json:"prompt_version"`
	Settings      map[string]string `json:"settings,omitempty"`
}

// DeterministicGenerator adapts the prototype provider to the evaluation API.
type DeterministicGenerator struct{}

func (DeterministicGenerator) Metadata() ProviderMetadata {
	return ProviderMetadata{
		Name:          "deterministic",
		Model:         "metadata-template",
		Quantization:  "not_applicable",
		Runtime:       "builtin",
		PromptVersion: "none",
		Settings:      map[string]string{"staged_metadata_limit": fmt.Sprint(candidate.MaxMetadata)},
	}
}

func (DeterministicGenerator) Generate(ctx context.Context, cwd string) ([]candidate.Candidate, error) {
	return candidate.Generate(ctx, cwd)
}

// DaemonGenerator evaluates the exact daemon preparation path in a private
// temporary cache. It disables fallback so a model's result cannot be confused
// with the deterministic control.
type DaemonGenerator struct {
	settings config.Settings
	metadata ProviderMetadata
}

func NewDaemonGenerator(settings config.Settings) (*DaemonGenerator, error) {
	backend, err := provider.New(settings)
	if err != nil {
		return nil, err
	}
	settings.ProviderFallback = "none"
	return &DaemonGenerator{settings: settings, metadata: providerMetadata(backend.Metadata())}, nil
}

func NewDeterministicDaemonGenerator() (*DaemonGenerator, error) {
	settings := config.Default()
	settings.ProviderFallback = "none"
	return NewDaemonGenerator(settings)
}

func (generator *DaemonGenerator) Metadata() ProviderMetadata {
	metadata := generator.metadata
	metadata.Settings = copySettings(metadata.Settings)
	return metadata
}

func (generator *DaemonGenerator) Preflight(ctx context.Context) error {
	if generator.settings.Provider != "ollama" {
		return nil
	}
	ollama, err := provider.NewOllama(provider.DefaultOllamaURL, generator.settings.ProviderModel, generator.settings.ProviderTimeout)
	if err != nil {
		return err
	}
	models, err := ollama.Models(ctx)
	if err != nil {
		return fmt.Errorf("discover local Ollama models: %w", err)
	}
	for _, model := range models {
		if model.Name != generator.settings.ProviderModel && model.Model != generator.settings.ProviderModel {
			continue
		}
		generator.metadata.ModelDigest = model.Digest
		generator.metadata.ModelSize = model.Size
		break
	}
	if generator.metadata.ModelDigest == "" {
		return fmt.Errorf("requested Ollama model %q is unavailable", generator.settings.ProviderModel)
	}
	if version, versionErr := exec.CommandContext(ctx, "ollama", "--version").Output(); versionErr == nil {
		generator.metadata.Settings = copySettings(generator.metadata.Settings)
		generator.metadata.Settings["runtime_cli_version"] = strings.TrimSpace(string(version))
	}
	return nil
}

func (generator *DaemonGenerator) Generate(ctx context.Context, cwd string) ([]candidate.Candidate, error) {
	generated, err := generator.GenerateDetailed(ctx, cwd)
	return generated.Candidates, err
}

func (generator *DaemonGenerator) GenerateDetailed(ctx context.Context, cwd string) (Generated, error) {
	cache, err := os.MkdirTemp("", "zsh-git-inlay-evaluation-cache-")
	if err != nil {
		return Generated{}, err
	}
	defer os.RemoveAll(cache)
	record, err := daemon.PrepareForEvaluation(ctx, generator.settings, cwd, cache)
	if err != nil {
		return Generated{}, err
	}
	updated := providerMetadata(record.Provider)
	updated.ModelDigest, updated.ModelSize = generator.metadata.ModelDigest, generator.metadata.ModelSize
	if version := generator.metadata.Settings["runtime_cli_version"]; version != "" {
		updated.Settings["runtime_cli_version"] = version
	}
	generator.metadata = updated
	return Generated{Candidates: record.Candidates, Grounding: record.Grounding}, nil
}

func providerMetadata(metadata provider.Metadata) ProviderMetadata {
	return ProviderMetadata{Name: metadata.Name, Model: metadata.Model, Quantization: metadata.Quantization, Runtime: metadata.Runtime, PromptVersion: metadata.PromptVersion, Settings: copySettings(metadata.Settings)}
}

func copySettings(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

// ProviderGenerator compiles the same bounded staged context used by the
// daemon before invoking a local provider for administrative evaluation.
// Construction has no network effect; Generate remains caller-controlled.
type ProviderGenerator struct{ Provider provider.Provider }

func NewOllamaGenerator(model string, timeout time.Duration) (*DaemonGenerator, error) {
	settings := config.Default()
	settings.Provider, settings.ProviderModel, settings.ProviderTimeout, settings.ProviderFallback = "ollama", model, timeout, "none"
	return NewDaemonGenerator(settings)
}

func (generator ProviderGenerator) Metadata() ProviderMetadata {
	if generator.Provider == nil {
		return ProviderMetadata{}
	}
	metadata := generator.Provider.Metadata()
	return ProviderMetadata{Name: metadata.Name, Model: metadata.Model, Quantization: metadata.Quantization, Runtime: metadata.Runtime, PromptVersion: metadata.PromptVersion, Settings: metadata.Settings}
}

func (generator ProviderGenerator) Generate(ctx context.Context, cwd string) ([]candidate.Candidate, error) {
	if generator.Provider == nil {
		return nil, errors.New("evaluation provider is required")
	}
	metadata := generator.Provider.Metadata()
	if metadata.Name != "ollama" {
		return nil, fmt.Errorf("evaluation provider %q is unsupported", metadata.Name)
	}
	state, err := gitstate.Snapshot(ctx, cwd)
	if err != nil {
		return nil, err
	}
	if state.Availability != gitstate.Ready {
		return nil, fmt.Errorf("evaluation staged state is %s", state.Availability)
	}
	compiled, err := repoctx.Compile(ctx, cwd, state, metadata.Name)
	if err != nil {
		return nil, err
	}
	response, err := generator.Provider.Generate(ctx, provider.Request{CWD: cwd, Context: compiled.Prompt(), ContextFingerprint: state.ContextFingerprint})
	if err != nil {
		return nil, err
	}
	return response.ToCandidates()
}

type Corpus struct {
	Version   string    `json:"version"`
	Digest    string    `json:"digest"`
	Cases     []Fixture `json:"cases"`
	Partition string    `json:"-"`
}

type Fixture struct {
	ID               string   `json:"id"`
	Partition        string   `json:"partition"`
	ReferenceSubject string   `json:"reference_subject"`
	Convention       string   `json:"convention,omitempty"`
	AllowedScopes    []string `json:"allowed_scopes,omitempty"`
	SubjectLength    int      `json:"subject_length,omitempty"`
	Components       []string `json:"components"`
	IssueIDs         []string `json:"issue_ids,omitempty"`
	SupportedClaims  []string `json:"supported_claims,omitempty"`
	Changes          []Change `json:"changes"`
}

type Change struct {
	Status       string `json:"status"`
	Path         string `json:"path"`
	PreviousPath string `json:"previous_path,omitempty"`
	Content      string `json:"content,omitempty"`
}

type Options struct {
	Timeout time.Duration
	Limit   int
}

func DefaultOptions() Options { return Options{Timeout: 10 * time.Second, Limit: 50} }

type Report struct {
	SchemaVersion string           `json:"schema_version"`
	GeneratedAt   time.Time        `json:"generated_at"`
	CorpusVersion string           `json:"corpus_version"`
	CorpusDigest  string           `json:"corpus_digest"`
	Source        Source           `json:"source"`
	Provider      ProviderMetadata `json:"provider"`
	Environment   Environment      `json:"environment"`
	Settings      Settings         `json:"settings"`
	Summary       Summary          `json:"summary"`
	Cases         []Result         `json:"cases"`
	ManualReview  string           `json:"manual_review"`
}

type Source struct {
	Kind          string `json:"kind"`
	Partition     string `json:"partition,omitempty"`
	Eligible      int    `json:"eligible_commits,omitempty"`
	SkippedMerges int    `json:"skipped_merges,omitempty"`
}

type Environment struct {
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	GoVersion string `json:"go_version"`
	CPUs      int    `json:"cpus"`
}

type Settings struct {
	Timeout string `json:"timeout"`
	Limit   int    `json:"limit"`
}

type Summary struct {
	Cases                   int     `json:"cases"`
	CandidateCount          int     `json:"candidate_count"`
	ValidOutput             Check   `json:"valid_output_schema"`
	Conventional            Check   `json:"conventional_commit"`
	AllowedScope            Check   `json:"allowed_scope"`
	SubjectLength           Check   `json:"subject_length"`
	ChangedComponent        Check   `json:"changed_component"`
	UnsupportedComponent    Check   `json:"unsupported_component"`
	UnsupportedIssue        Check   `json:"unsupported_issue_identifier"`
	UnsupportedTestOutcome  Check   `json:"unsupported_test_outcome_claim"`
	UnsupportedBehavior     Check   `json:"unsupported_behavioral_claim"`
	StructuralGrounding     Check   `json:"structural_grounding_coverage"`
	DaemonGrounding         Check   `json:"daemon_grounding_coverage"`
	Repeatability           Check   `json:"repeated_generation_match"`
	AverageDiversity        float64 `json:"average_diversity"`
	ColdLatencyMilliseconds float64 `json:"cold_latency_ms"`
	ColdP50Milliseconds     float64 `json:"cold_p50_ms"`
	ColdP95Milliseconds     float64 `json:"cold_p95_ms"`
	WarmLatencyMilliseconds float64 `json:"warm_latency_ms"`
	WarmP50Milliseconds     float64 `json:"warm_p50_ms"`
	WarmP95Milliseconds     float64 `json:"warm_p95_ms"`
	ApproxAllocatedBytes    uint64  `json:"approx_allocated_bytes"`
	TimeoutRate             float64 `json:"timeout_rate"`
	ErrorRate               float64 `json:"error_rate"`
}

type Check struct {
	Checked int     `json:"checked"`
	Passed  int     `json:"passed"`
	Rate    float64 `json:"rate"`
}

type Result struct {
	ID               string          `json:"id"`
	SourceCommit     string          `json:"source_commit,omitempty"`
	ReferenceSubject string          `json:"reference_subject"`
	CandidateCount   int             `json:"candidate_count"`
	Automatic        AutomaticChecks `json:"automatic"`
	Generation       Generation      `json:"generation"`
}

type AutomaticChecks struct {
	ValidOutput            Check   `json:"valid_output_schema"`
	Conventional           Check   `json:"conventional_commit"`
	AllowedScope           Check   `json:"allowed_scope"`
	SubjectLength          Check   `json:"subject_length"`
	ChangedComponent       Check   `json:"changed_component"`
	UnsupportedComponent   Check   `json:"unsupported_component"`
	UnsupportedIssue       Check   `json:"unsupported_issue_identifier"`
	UnsupportedTestOutcome Check   `json:"unsupported_test_outcome_claim"`
	UnsupportedBehavior    Check   `json:"unsupported_behavioral_claim"`
	StructuralGrounding    Check   `json:"structural_grounding_coverage"`
	DaemonGrounding        Check   `json:"daemon_grounding_coverage"`
	Repeatability          Check   `json:"repeated_generation_match"`
	Diversity              float64 `json:"diversity"`
}

type Generation struct {
	ColdMilliseconds float64 `json:"cold_ms"`
	WarmMilliseconds float64 `json:"warm_ms"`
	ApproxAllocBytes uint64  `json:"approx_alloc_bytes"`
	ColdTimeout      bool    `json:"cold_timeout"`
	WarmTimeout      bool    `json:"warm_timeout"`
	ColdError        string  `json:"cold_error,omitempty"`
	WarmError        string  `json:"warm_error,omitempty"`
}

// LoadCorpus returns the public, synthetic corpus packaged with the binary.
func LoadCorpus() (Corpus, error) {
	var corpus Corpus
	if err := json.Unmarshal(embeddedCorpus, &corpus); err != nil {
		return Corpus{}, fmt.Errorf("decode embedded evaluation corpus: %w", err)
	}
	if corpus.Version != CorpusVersion || len(corpus.Cases) == 0 {
		return Corpus{}, errors.New("invalid embedded evaluation corpus")
	}
	for _, fixture := range corpus.Cases {
		if err := validFixture(fixture); err != nil {
			return Corpus{}, fmt.Errorf("invalid fixture %q: %w", fixture.ID, err)
		}
	}
	digest := sha256.Sum256(embeddedCorpus)
	corpus.Digest = hex.EncodeToString(digest[:])
	return corpus, nil
}

// Partition returns an explicit development or held-out slice of the public
// corpus. "all" retains every case and is useful for regression coverage, not
// model-selection reporting.
func Partition(corpus Corpus, partition string) (Corpus, error) {
	partition = strings.ReplaceAll(partition, "-", "_")
	if partition == "" || partition == "all" {
		corpus.Partition = "all"
		return corpus, nil
	}
	if partition != DevelopmentSet && partition != HeldOutSet {
		return Corpus{}, fmt.Errorf("unsupported evaluation partition %q", partition)
	}
	filtered := Corpus{Version: corpus.Version, Digest: corpus.Digest, Partition: partition}
	for _, fixture := range corpus.Cases {
		if fixture.Partition == partition {
			filtered.Cases = append(filtered.Cases, fixture)
		}
	}
	if len(filtered.Cases) == 0 {
		return Corpus{}, fmt.Errorf("evaluation partition %q has no cases", partition)
	}
	return filtered, nil
}

func EvaluateCorpus(ctx context.Context, corpus Corpus, generator Generator, options Options) (Report, error) {
	if generator == nil {
		return Report{}, errors.New("evaluation generator is required")
	}
	options = normaliseOptions(options)
	if preflight, ok := generator.(PreflightGenerator); ok {
		if err := preflight.Preflight(ctx); err != nil {
			return Report{}, err
		}
	}
	report := newReport(corpus.Version, Source{Kind: "synthetic_fixture", Partition: corpus.Partition}, generator, options)
	report.CorpusDigest = corpus.Digest
	for _, fixture := range corpus.Cases {
		repository, err := fixtureRepository(ctx, fixture)
		if err != nil {
			return Report{}, err
		}
		result := evaluate(ctx, repository, fixture, "", generator, options)
		if err := os.RemoveAll(repository); err != nil {
			return Report{}, fmt.Errorf("remove synthetic evaluation repository: %w", err)
		}
		report.Cases = append(report.Cases, result)
	}
	report.Summary = summarize(report.Cases)
	report.Provider = generator.Metadata()
	return report, nil
}

// ReplayHistory reconstructs each eligible parent-to-commit staged index in a
// temporary repository. It never checks a private working tree into this repo
// and never writes a raw diff to a report.
func ReplayHistory(ctx context.Context, repository string, generator Generator, options Options) (Report, error) {
	if generator == nil {
		return Report{}, errors.New("evaluation generator is required")
	}
	options = normaliseOptions(options)
	if preflight, ok := generator.(PreflightGenerator); ok {
		if err := preflight.Preflight(ctx); err != nil {
			return Report{}, err
		}
	}
	repository, err := filepath.Abs(repository)
	if err != nil {
		return Report{}, err
	}
	commits, skipped, err := eligibleCommits(ctx, repository, options.Limit)
	if err != nil {
		return Report{}, err
	}
	report := newReport("local-history-v1", Source{Kind: "local_history", Eligible: len(commits), SkippedMerges: skipped}, generator, options)
	objects, err := objectDirectory(ctx, repository)
	if err != nil {
		return Report{}, err
	}
	for _, commit := range commits {
		fixture, err := historyFixture(ctx, repository, commit)
		if err != nil {
			return Report{}, err
		}
		temporary, err := replayRepository(ctx, repository, objects, commit.parent, commit.hash)
		if err != nil {
			return Report{}, err
		}
		result := evaluate(ctx, temporary, fixture, commit.hash, generator, options)
		if err := os.RemoveAll(temporary); err != nil {
			return Report{}, fmt.Errorf("remove replay repository: %w", err)
		}
		report.Cases = append(report.Cases, result)
	}
	report.Summary = summarize(report.Cases)
	report.Provider = generator.Metadata()
	return report, nil
}

func newReport(corpusVersion string, source Source, generator Generator, options Options) Report {
	return Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		CorpusVersion: corpusVersion,
		Source:        source,
		Provider:      generator.Metadata(),
		Environment:   Environment{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, GoVersion: runtime.Version(), CPUs: runtime.NumCPU()},
		Settings:      Settings{Timeout: options.Timeout.String(), Limit: options.Limit},
		ManualReview:  "Automatic checks measure structural and evidence-token properties only. Review candidate usefulness, factual adequacy, and the historical subject independently; string similarity is deliberately not a quality score.",
	}
}

func evaluate(parent context.Context, repository string, fixture Fixture, commit string, generator Generator, options Options) Result {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	cold, coldDuration, coldErr, coldTimeout := generate(parent, repository, generator, options.Timeout)
	runtime.ReadMemStats(&after)
	warm, warmDuration, warmErr, warmTimeout := generate(parent, repository, generator, options.Timeout)
	checks := assess(cold.Candidates, fixture)
	checks.DaemonGrounding = daemonGrounding(cold.Grounding)
	checks.Repeatability = check(checks.Repeatability, coldErr == nil && warmErr == nil && sameCandidates(cold.Candidates, warm.Candidates))
	return Result{
		ID: fixture.ID, SourceCommit: commit, ReferenceSubject: fixture.ReferenceSubject, CandidateCount: len(cold.Candidates), Automatic: checks,
		Generation: Generation{ColdMilliseconds: milliseconds(coldDuration), WarmMilliseconds: milliseconds(warmDuration), ApproxAllocBytes: after.TotalAlloc - before.TotalAlloc, ColdTimeout: coldTimeout, WarmTimeout: warmTimeout, ColdError: errorString(coldErr), WarmError: errorString(warmErr)},
	}
}

func generate(parent context.Context, repository string, generator Generator, timeout time.Duration) (Generated, time.Duration, error, bool) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	started := time.Now()
	generated := Generated{}
	var err error
	if detailed, ok := generator.(DetailedGenerator); ok {
		generated, err = detailed.GenerateDetailed(ctx, repository)
	} else {
		generated.Candidates, err = generator.Generate(ctx, repository)
	}
	duration := time.Since(started)
	return generated, duration, err, errors.Is(ctx.Err(), context.DeadlineExceeded)
}

func daemonGrounding(reports []grounding.Result) Check {
	result := Check{}
	for _, report := range reports {
		result = check(result, report.State == grounding.Grounded || report.State == grounding.PartiallyGrounded)
	}
	return result
}

func sameCandidates(left, right []candidate.Candidate) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Message != right[index].Message {
			return false
		}
	}
	return true
}

func assess(values []candidate.Candidate, fixture Fixture) AutomaticChecks {
	checks := AutomaticChecks{}
	unique := map[string]bool{}
	components := stringSet(fixture.Components)
	allowedScopes := stringSet(fixture.AllowedScopes)
	issues := stringSet(fixture.IssueIDs)
	claims := stringSet(fixture.SupportedClaims)
	for _, value := range values {
		validOutput := candidate.Valid(value)
		checks.ValidOutput = check(checks.ValidOutput, validOutput)
		match := conventionalSubject.FindStringSubmatch(value.Message)
		conventional := true
		if fixture.Convention == "conventional" {
			conventional = match != nil
			checks.Conventional = check(checks.Conventional, conventional)
		}
		subjectLength := true
		if fixture.SubjectLength > 0 {
			subjectLength = len(value.Message) <= fixture.SubjectLength
			checks.SubjectLength = check(checks.SubjectLength, subjectLength)
		}
		scope := ""
		if len(match) >= 4 {
			scope = match[3]
		}
		allowedScope := true
		if len(allowedScopes) > 0 {
			allowedScope = scope != "" && allowedScopes[scope]
			checks.AllowedScope = check(checks.AllowedScope, allowedScope)
		}
		changedComponent, unsupportedComponent := true, true
		if len(components) > 0 {
			changedComponent = scope != "" && components[scope]
			unsupportedComponent = scope == "" || components[scope]
			checks.ChangedComponent = check(checks.ChangedComponent, changedComponent)
			checks.UnsupportedComponent = check(checks.UnsupportedComponent, unsupportedComponent)
		}
		unsupportedIssue := false
		for _, identifier := range issueIdentifier.FindAllString(value.Message, -1) {
			if !issues[identifier] {
				unsupportedIssue = true
			}
		}
		checks.UnsupportedIssue = check(checks.UnsupportedIssue, !unsupportedIssue)
		unsupportedTestOutcome := testOutcomeClaim.MatchString(strings.ToLower(value.Message))
		checks.UnsupportedTestOutcome = check(checks.UnsupportedTestOutcome, !unsupportedTestOutcome)
		unsupportedClaim := false
		for _, claim := range behavioralClaim.FindAllString(strings.ToLower(value.Message), -1) {
			if !claims[claim] {
				unsupportedClaim = true
			}
		}
		checks.UnsupportedBehavior = check(checks.UnsupportedBehavior, !unsupportedClaim)
		structurallyGrounded := validOutput && conventional && subjectLength && allowedScope && changedComponent && unsupportedComponent && !unsupportedIssue && !unsupportedTestOutcome && !unsupportedClaim
		checks.StructuralGrounding = check(checks.StructuralGrounding, structurallyGrounded)
		unique[value.Message] = true
	}
	if len(values) > 0 {
		checks.Diversity = float64(len(unique)) / float64(len(values))
	}
	return checks
}

func summarize(results []Result) Summary {
	summary := Summary{Cases: len(results)}
	var cold, warm float64
	coldSamples, warmSamples := make([]float64, 0, len(results)), make([]float64, 0, len(results))
	var calls, timeouts, errorsCount int
	for _, result := range results {
		summary.CandidateCount += result.CandidateCount
		summary.ValidOutput = merge(summary.ValidOutput, result.Automatic.ValidOutput)
		summary.Conventional = merge(summary.Conventional, result.Automatic.Conventional)
		summary.AllowedScope = merge(summary.AllowedScope, result.Automatic.AllowedScope)
		summary.SubjectLength = merge(summary.SubjectLength, result.Automatic.SubjectLength)
		summary.ChangedComponent = merge(summary.ChangedComponent, result.Automatic.ChangedComponent)
		summary.UnsupportedComponent = merge(summary.UnsupportedComponent, result.Automatic.UnsupportedComponent)
		summary.UnsupportedIssue = merge(summary.UnsupportedIssue, result.Automatic.UnsupportedIssue)
		summary.UnsupportedTestOutcome = merge(summary.UnsupportedTestOutcome, result.Automatic.UnsupportedTestOutcome)
		summary.UnsupportedBehavior = merge(summary.UnsupportedBehavior, result.Automatic.UnsupportedBehavior)
		summary.StructuralGrounding = merge(summary.StructuralGrounding, result.Automatic.StructuralGrounding)
		summary.DaemonGrounding = merge(summary.DaemonGrounding, result.Automatic.DaemonGrounding)
		summary.Repeatability = merge(summary.Repeatability, result.Automatic.Repeatability)
		summary.AverageDiversity += result.Automatic.Diversity
		cold += result.Generation.ColdMilliseconds
		warm += result.Generation.WarmMilliseconds
		coldSamples = append(coldSamples, result.Generation.ColdMilliseconds)
		warmSamples = append(warmSamples, result.Generation.WarmMilliseconds)
		summary.ApproxAllocatedBytes += result.Generation.ApproxAllocBytes
		calls += 2
		if result.Generation.ColdTimeout {
			timeouts++
		}
		if result.Generation.WarmTimeout {
			timeouts++
		}
		if result.Generation.ColdError != "" {
			errorsCount++
		}
		if result.Generation.WarmError != "" {
			errorsCount++
		}
	}
	if len(results) > 0 {
		summary.AverageDiversity /= float64(len(results))
		summary.ColdLatencyMilliseconds = cold / float64(len(results))
		summary.ColdP50Milliseconds = percentile(coldSamples, 0.50)
		summary.ColdP95Milliseconds = percentile(coldSamples, 0.95)
		summary.WarmLatencyMilliseconds = warm / float64(len(results))
		summary.WarmP50Milliseconds = percentile(warmSamples, 0.50)
		summary.WarmP95Milliseconds = percentile(warmSamples, 0.95)
	}
	if calls > 0 {
		summary.TimeoutRate = float64(timeouts) / float64(calls)
		summary.ErrorRate = float64(errorsCount) / float64(calls)
	}
	return summary
}

func percentile(values []float64, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	index := int(float64(len(ordered)-1)*quantile + 0.5)
	return ordered[index]
}

func check(value Check, passed bool) Check {
	value.Checked++
	if passed {
		value.Passed++
	}
	value.Rate = float64(value.Passed) / float64(value.Checked)
	return value
}

func merge(left, right Check) Check {
	left.Checked += right.Checked
	left.Passed += right.Passed
	if left.Checked > 0 {
		left.Rate = float64(left.Passed) / float64(left.Checked)
	}
	return left
}

func normaliseOptions(options Options) Options {
	if options.Timeout <= 0 {
		options.Timeout = DefaultOptions().Timeout
	}
	if options.Limit <= 0 {
		options.Limit = DefaultOptions().Limit
	}
	return options
}

func milliseconds(value time.Duration) float64 { return float64(value) / float64(time.Millisecond) }

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func validFixture(fixture Fixture) error {
	if fixture.ID == "" || fixture.ReferenceSubject == "" || len(fixture.Components) == 0 || len(fixture.Changes) == 0 {
		return errors.New("missing required fixture fields")
	}
	if fixture.Partition != DevelopmentSet && fixture.Partition != HeldOutSet {
		return fmt.Errorf("invalid fixture partition %q", fixture.Partition)
	}
	for _, change := range fixture.Changes {
		if !validPath(change.Path) {
			return fmt.Errorf("invalid path %q", change.Path)
		}
		switch change.Status {
		case "A", "M", "D":
		case "R":
			if !validPath(change.PreviousPath) {
				return fmt.Errorf("invalid rename source %q", change.PreviousPath)
			}
		default:
			return fmt.Errorf("unsupported status %q", change.Status)
		}
	}
	return nil
}

func validPath(value string) bool {
	return value != "" && !filepath.IsAbs(value) && !strings.Contains(value, "\x00") && !strings.HasPrefix(filepath.Clean(value), "..")
}

func fixtureRepository(ctx context.Context, fixture Fixture) (string, error) {
	directory, err := os.MkdirTemp("", "zsh-git-inlay-evaluation-fixture-")
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) { _ = os.RemoveAll(directory); return "", err }
	if err := git(ctx, directory, nil, "init", "-q", "-b", "main"); err != nil {
		return fail(err)
	}
	if err := git(ctx, directory, nil, "config", "user.name", "Evaluation"); err != nil {
		return fail(err)
	}
	if err := git(ctx, directory, nil, "config", "user.email", "evaluation@example.invalid"); err != nil {
		return fail(err)
	}
	needsBase := false
	for _, change := range fixture.Changes {
		if change.Status == "M" || change.Status == "D" || change.Status == "R" {
			needsBase = true
			break
		}
	}
	if needsBase {
		for _, change := range fixture.Changes {
			if change.Status == "M" || change.Status == "D" {
				if err := writeFile(directory, change.Path, "previous synthetic content\n"); err != nil {
					return fail(err)
				}
			}
			if change.Status == "R" {
				if err := writeFile(directory, change.PreviousPath, "previous synthetic content\n"); err != nil {
					return fail(err)
				}
			}
		}
		if err := git(ctx, directory, nil, "add", "--all"); err != nil {
			return fail(err)
		}
		if err := git(ctx, directory, nil, "commit", "-qm", "synthetic baseline"); err != nil {
			return fail(err)
		}
	}
	for _, change := range fixture.Changes {
		switch change.Status {
		case "A", "M":
			if err := writeFile(directory, change.Path, change.Content); err != nil {
				return fail(err)
			}
		case "D":
			if err := os.Remove(filepath.Join(directory, change.Path)); err != nil {
				return fail(err)
			}
		case "R":
			if err := git(ctx, directory, nil, "mv", "--", change.PreviousPath, change.Path); err != nil {
				return fail(err)
			}
			if change.Content != "" {
				if err := writeFile(directory, change.Path, change.Content); err != nil {
					return fail(err)
				}
			}
		}
	}
	if err := git(ctx, directory, nil, "add", "--all"); err != nil {
		return fail(err)
	}
	return directory, nil
}

func writeFile(root, name, content string) error {
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

type historyCommit struct{ hash, parent string }

func eligibleCommits(ctx context.Context, repository string, limit int) ([]historyCommit, int, error) {
	output, err := gitOutput(ctx, repository, nil, "rev-list", "--topo-order", "--reverse", fmt.Sprintf("--max-count=%d", limit*3), "HEAD")
	if err != nil {
		return nil, 0, fmt.Errorf("select historical commits: %w", err)
	}
	commits, skipped := make([]historyCommit, 0, limit), 0
	for _, hash := range strings.Fields(output) {
		parents, parentErr := gitOutput(ctx, repository, nil, "rev-list", "--parents", "-n", "1", hash)
		if parentErr != nil {
			return nil, 0, parentErr
		}
		parts := strings.Fields(parents)
		if len(parts) != 2 {
			skipped++
			continue
		}
		commits = append(commits, historyCommit{hash: parts[0], parent: parts[1]})
		if len(commits) == limit {
			break
		}
	}
	return commits, skipped, nil
}

func objectDirectory(ctx context.Context, repository string) (string, error) {
	gitDir, err := gitOutput(ctx, repository, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("locate repository objects: %w", err)
	}
	return filepath.Join(strings.TrimSpace(gitDir), "objects"), nil
}

func historyFixture(ctx context.Context, repository string, commit historyCommit) (Fixture, error) {
	subject, err := gitOutput(ctx, repository, nil, "log", "-1", "--format=%s", commit.hash)
	if err != nil {
		return Fixture{}, err
	}
	paths, err := gitOutput(ctx, repository, nil, "diff", "--name-only", "-z", commit.parent, commit.hash)
	if err != nil {
		return Fixture{}, err
	}
	components := map[string]bool{}
	for _, path := range strings.Split(paths, "\x00") {
		if path == "" {
			continue
		}
		components[strings.Split(path, "/")[0]] = true
	}
	values := make([]string, 0, len(components))
	for component := range components {
		values = append(values, component)
	}
	sort.Strings(values)
	if len(values) == 0 {
		values = []string{"repo"}
	}
	return Fixture{ID: "commit-" + commit.hash[:12], ReferenceSubject: subject, Components: values, Changes: []Change{{Status: "A", Path: "history"}}}, nil
}

func replayRepository(ctx context.Context, source, objects, parent, commit string) (string, error) {
	directory, err := os.MkdirTemp("", "zsh-git-inlay-evaluation-history-")
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) { _ = os.RemoveAll(directory); return "", err }
	environment := []string{"GIT_ALTERNATE_OBJECT_DIRECTORIES=" + objects}
	if err := git(ctx, directory, environment, "init", "-q", "-b", "main"); err != nil {
		return fail(err)
	}
	alternates := filepath.Join(directory, ".git", "objects", "info", "alternates")
	if err := os.MkdirAll(filepath.Dir(alternates), 0o700); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(alternates, []byte(objects+"\n"), 0o600); err != nil {
		return fail(err)
	}
	if err := git(ctx, directory, environment, "read-tree", parent+"^{tree}"); err != nil {
		return fail(err)
	}
	diff := exec.CommandContext(ctx, "git", "-C", source, "diff", "--binary", "--full-index", "--no-ext-diff", parent, commit)
	diff.Env = gitEnvironment(environment)
	pipe, err := diff.StdoutPipe()
	if err != nil {
		return fail(err)
	}
	apply := exec.CommandContext(ctx, "git", "-C", directory, "apply", "--cached", "--whitespace=nowarn")
	apply.Env, apply.Stdin = gitEnvironment(environment), pipe
	if err := diff.Start(); err != nil {
		return fail(err)
	}
	applyErr := apply.Run()
	diffErr := diff.Wait()
	if applyErr != nil || diffErr != nil {
		return fail(fmt.Errorf("reconstruct staged history: apply=%v diff=%v", applyErr, diffErr))
	}
	return directory, nil
}

func git(ctx context.Context, directory string, environment []string, args ...string) error {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...)
	command.Env = gitEnvironment(environment)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func gitOutput(ctx context.Context, directory string, environment []string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...)
	command.Env = gitEnvironment(environment)
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSuffix(string(output), "\n"), nil
}

func gitEnvironment(additions []string) []string {
	environment := make([]string, 0, len(os.Environ())+3+len(additions))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") {
			continue
		}
		environment = append(environment, entry)
	}
	environment = append(environment, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0")
	return append(environment, additions...)
}
