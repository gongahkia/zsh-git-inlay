// Package evaluation runs reproducible candidate evaluations away from ZLE.
package evaluation

import (
	"context"
	_ "embed"
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
)

const SchemaVersion = "v1"

//go:embed testdata/corpus-v1.json
var embeddedCorpus []byte

var conventionalSubject = regexp.MustCompile(`^(feat|fix|docs|test|refactor|build|chore|ci|perf|style|revert)(\(([a-z0-9._/-]+)\))?!?: .+$`)
var issueIdentifier = regexp.MustCompile(`(?:#[0-9]+|\b[A-Z][A-Z0-9]+-[0-9]+\b)`)
var behavioralClaim = regexp.MustCompile(`\b(fix|prevent|ensure|avoid|resolve|eliminate)\b`)

// Generator is intentionally small so the corpus can exercise the built-in
// generator today and model providers once they are introduced.
type Generator interface {
	Metadata() ProviderMetadata
	Generate(context.Context, string) ([]candidate.Candidate, error)
}

type ProviderMetadata struct {
	Name          string            `json:"name"`
	Model         string            `json:"model"`
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

type Corpus struct {
	Version string    `json:"version"`
	Cases   []Fixture `json:"cases"`
}

type Fixture struct {
	ID               string   `json:"id"`
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
	UnsupportedBehavior     Check   `json:"unsupported_behavioral_claim"`
	AverageDiversity        float64 `json:"average_diversity"`
	ColdLatencyMilliseconds float64 `json:"cold_latency_ms"`
	WarmLatencyMilliseconds float64 `json:"warm_latency_ms"`
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
	ValidOutput          Check   `json:"valid_output_schema"`
	Conventional         Check   `json:"conventional_commit"`
	AllowedScope         Check   `json:"allowed_scope"`
	SubjectLength        Check   `json:"subject_length"`
	ChangedComponent     Check   `json:"changed_component"`
	UnsupportedComponent Check   `json:"unsupported_component"`
	UnsupportedIssue     Check   `json:"unsupported_issue_identifier"`
	UnsupportedBehavior  Check   `json:"unsupported_behavioral_claim"`
	Diversity            float64 `json:"diversity"`
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
	if corpus.Version != SchemaVersion || len(corpus.Cases) == 0 {
		return Corpus{}, errors.New("invalid embedded evaluation corpus")
	}
	for _, fixture := range corpus.Cases {
		if err := validFixture(fixture); err != nil {
			return Corpus{}, fmt.Errorf("invalid fixture %q: %w", fixture.ID, err)
		}
	}
	return corpus, nil
}

func EvaluateCorpus(ctx context.Context, corpus Corpus, generator Generator, options Options) (Report, error) {
	if generator == nil {
		return Report{}, errors.New("evaluation generator is required")
	}
	options = normaliseOptions(options)
	report := newReport(corpus.Version, Source{Kind: "synthetic_fixture"}, generator, options)
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
	coldCandidates, coldDuration, coldErr, coldTimeout := generate(parent, repository, generator, options.Timeout)
	runtime.ReadMemStats(&after)
	_, warmDuration, warmErr, warmTimeout := generate(parent, repository, generator, options.Timeout)
	checks := assess(coldCandidates, fixture)
	return Result{
		ID: fixture.ID, SourceCommit: commit, ReferenceSubject: fixture.ReferenceSubject, CandidateCount: len(coldCandidates), Automatic: checks,
		Generation: Generation{ColdMilliseconds: milliseconds(coldDuration), WarmMilliseconds: milliseconds(warmDuration), ApproxAllocBytes: after.TotalAlloc - before.TotalAlloc, ColdTimeout: coldTimeout, WarmTimeout: warmTimeout, ColdError: errorString(coldErr), WarmError: errorString(warmErr)},
	}
}

func generate(parent context.Context, repository string, generator Generator, timeout time.Duration) ([]candidate.Candidate, time.Duration, error, bool) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	started := time.Now()
	values, err := generator.Generate(ctx, repository)
	duration := time.Since(started)
	return values, duration, err, errors.Is(ctx.Err(), context.DeadlineExceeded)
}

func assess(values []candidate.Candidate, fixture Fixture) AutomaticChecks {
	checks := AutomaticChecks{}
	unique := map[string]bool{}
	components := stringSet(fixture.Components)
	allowedScopes := stringSet(fixture.AllowedScopes)
	issues := stringSet(fixture.IssueIDs)
	claims := stringSet(fixture.SupportedClaims)
	for _, value := range values {
		checks.ValidOutput = check(checks.ValidOutput, candidate.Valid(value))
		match := conventionalSubject.FindStringSubmatch(value.Message)
		if fixture.Convention == "conventional" {
			checks.Conventional = check(checks.Conventional, match != nil)
		}
		if fixture.SubjectLength > 0 {
			checks.SubjectLength = check(checks.SubjectLength, len(value.Message) <= fixture.SubjectLength)
		}
		scope := ""
		if len(match) >= 4 {
			scope = match[3]
		}
		if len(allowedScopes) > 0 {
			checks.AllowedScope = check(checks.AllowedScope, scope != "" && allowedScopes[scope])
		}
		if len(components) > 0 {
			checks.ChangedComponent = check(checks.ChangedComponent, scope != "" && components[scope])
			checks.UnsupportedComponent = check(checks.UnsupportedComponent, scope == "" || components[scope])
		}
		unsupportedIssue := false
		for _, identifier := range issueIdentifier.FindAllString(value.Message, -1) {
			if !issues[identifier] {
				unsupportedIssue = true
			}
		}
		checks.UnsupportedIssue = check(checks.UnsupportedIssue, !unsupportedIssue)
		unsupportedClaim := false
		for _, claim := range behavioralClaim.FindAllString(strings.ToLower(value.Message), -1) {
			if !claims[claim] {
				unsupportedClaim = true
			}
		}
		checks.UnsupportedBehavior = check(checks.UnsupportedBehavior, !unsupportedClaim)
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
		summary.UnsupportedBehavior = merge(summary.UnsupportedBehavior, result.Automatic.UnsupportedBehavior)
		summary.AverageDiversity += result.Automatic.Diversity
		cold += result.Generation.ColdMilliseconds
		warm += result.Generation.WarmMilliseconds
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
		summary.WarmLatencyMilliseconds = warm / float64(len(results))
	}
	if calls > 0 {
		summary.TimeoutRate = float64(timeouts) / float64(calls)
		summary.ErrorRate = float64(errorsCount) / float64(calls)
	}
	return summary
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
