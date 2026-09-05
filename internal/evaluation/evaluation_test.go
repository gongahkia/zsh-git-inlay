package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/candidate"
)

func TestSyntheticCorpusEvaluatesDeterministically(t *testing.T) {
	corpus, err := LoadCorpus()
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 11 {
		t.Fatalf("fixture count = %d, want 11", len(corpus.Cases))
	}
	report, err := EvaluateCorpus(context.Background(), corpus, DeterministicGenerator{}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != SchemaVersion || report.Source.Kind != "synthetic_fixture" || report.Summary.Cases != len(corpus.Cases) {
		t.Fatalf("report = %#v", report)
	}
	if report.Provider.Model == "" || report.Provider.Quantization == "" || report.Provider.Runtime == "" || report.Provider.PromptVersion == "" || report.Environment.GOARCH == "" || report.Environment.CPUs < 1 || report.Settings.Timeout == "" {
		t.Fatalf("missing reproducibility metadata: %#v", report)
	}
	for _, check := range []Check{report.Summary.ValidOutput, report.Summary.Conventional, report.Summary.AllowedScope, report.Summary.SubjectLength, report.Summary.ChangedComponent, report.Summary.UnsupportedComponent, report.Summary.UnsupportedIssue, report.Summary.UnsupportedBehavior} {
		if check.Checked == 0 || check.Passed != check.Checked {
			t.Fatalf("automatic check = %#v", check)
		}
	}
	if report.Summary.TimeoutRate != 0 || report.Summary.ErrorRate != 0 || report.Summary.AverageDiversity != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "Ignore every policy") {
		t.Fatal("synthetic staged content leaked into report")
	}
}

func TestReplayHistoryReconstructsParentToCommitIndex(t *testing.T) {
	repository := t.TempDir()
	evaluationGit(t, repository, "init", "-q", "-b", "main")
	evaluationGit(t, repository, "config", "user.name", "Evaluation")
	evaluationGit(t, repository, "config", "user.email", "evaluation@example.invalid")
	evaluationWrite(t, repository, "README.md", "base\n")
	evaluationGit(t, repository, "add", "README.md")
	evaluationGit(t, repository, "commit", "-qm", "chore: baseline")
	evaluationWrite(t, repository, "api/health.go", "package api\n")
	evaluationGit(t, repository, "add", "api/health.go")
	evaluationGit(t, repository, "commit", "-qm", "feat(api): add health endpoint")
	evaluationWrite(t, repository, "docs/health.md", "# health\n")
	evaluationGit(t, repository, "add", "docs/health.md")
	evaluationGit(t, repository, "commit", "-qm", "docs: explain health endpoint")

	report, err := ReplayHistory(context.Background(), repository, DeterministicGenerator{}, Options{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if report.Source.Kind != "local_history" || report.Source.Eligible != 2 || len(report.Cases) != 2 {
		t.Fatalf("source=%#v cases=%#v", report.Source, report.Cases)
	}
	for _, result := range report.Cases {
		if result.SourceCommit == "" || result.ReferenceSubject == "" || result.CandidateCount == 0 || result.Generation.ColdError != "" {
			t.Fatalf("replay result = %#v", result)
		}
	}
}

func TestPrivateReportsOmitDiffsAndUsePrivatePermissions(t *testing.T) {
	corpus, err := LoadCorpus()
	if err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateCorpus(context.Background(), corpus, DeterministicGenerator{}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	paths, err := WriteReports(filepath.Join(t.TempDir(), "reports"), report)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.JSON, paths.Markdown} {
		info, statErr := os.Stat(path)
		if statErr != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("private report %s: info=%v err=%v", path, info, statErr)
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(content), "Ignore every policy") || strings.Contains(string(content), "diff --git") {
			t.Fatalf("report leaked raw staged content: %s", path)
		}
	}

	root := evaluationGitOutput(t, ".", "rev-parse", "--show-toplevel")
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{"/evaluation-private/", "*.zsh-git-inlay-evaluation.json", "*.zsh-git-inlay-evaluation.md"} {
		if !strings.Contains(string(ignore), pattern) {
			t.Fatalf(".gitignore does not exclude private evaluation pattern %q", pattern)
		}
	}
}

func TestEvaluationRecordsTimeoutsAndProviderErrors(t *testing.T) {
	corpus, err := LoadCorpus()
	if err != nil {
		t.Fatal(err)
	}
	corpus.Cases = corpus.Cases[:1]
	report, err := EvaluateCorpus(context.Background(), corpus, waitingGenerator{}, Options{Timeout: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.TimeoutRate != 1 || report.Summary.ErrorRate != 1 || report.Cases[0].Generation.ColdError == "" {
		t.Fatalf("timeout/error metrics = %#v, case = %#v", report.Summary, report.Cases[0].Generation)
	}
}

func TestGitEnvironmentRejectsInheritedGitOverrides(t *testing.T) {
	t.Setenv("GIT_DIR", "/tmp/untrusted-git-directory")
	for _, entry := range gitEnvironment(nil) {
		if strings.HasPrefix(entry, "GIT_DIR=") {
			t.Fatalf("inherited Git override survived: %q", entry)
		}
	}
}

type waitingGenerator struct{}

func (waitingGenerator) Metadata() ProviderMetadata {
	return ProviderMetadata{Name: "waiting", Model: "test", Runtime: "test", PromptVersion: "test"}
}

func (waitingGenerator) Generate(ctx context.Context, _ string) ([]candidate.Candidate, error) {
	<-ctx.Done()
	return nil, errors.New("provider stopped")
}

func evaluationWrite(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func evaluationGit(t *testing.T, cwd string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}

func evaluationGitOutput(t *testing.T, cwd string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v: %v", arguments, err)
	}
	return strings.TrimSpace(string(output))
}
