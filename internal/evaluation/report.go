package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ReportPaths struct {
	JSON     string `json:"json"`
	Markdown string `json:"markdown"`
}

// WriteReports writes private local reports. Reports contain metrics and local
// reference subjects, never raw evaluation diffs.
func WriteReports(directory string, report Report) (ReportPaths, error) {
	if err := ensurePrivateDirectory(directory); err != nil {
		return ReportPaths{}, err
	}
	stamp := report.GeneratedAt.UTC().Format("20060102T150405.000000000Z")
	base := "evaluation-" + stamp + "-" + safeName(report.Source.Kind)
	paths := ReportPaths{JSON: filepath.Join(directory, base+".json"), Markdown: filepath.Join(directory, base+".md")}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return ReportPaths{}, err
	}
	if err := writePrivate(paths.JSON, append(encoded, '\n')); err != nil {
		return ReportPaths{}, err
	}
	if err := writePrivate(paths.Markdown, []byte(Markdown(report))); err != nil {
		_ = os.Remove(paths.JSON)
		return ReportPaths{}, err
	}
	return paths, nil
}

func Markdown(report Report) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# zsh-git-inlay evaluation report\n\n")
	fmt.Fprintf(&builder, "Generated: %s  \n", report.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&builder, "Corpus: %s  \n", report.CorpusVersion)
	fmt.Fprintf(&builder, "Source: %s  \n", report.Source.Kind)
	if report.Source.Partition != "" {
		fmt.Fprintf(&builder, "Partition: %s  \n", report.Source.Partition)
	}
	fmt.Fprintf(&builder, "Provider: %s / %s (%s)  \n\n", report.Provider.Name, report.Provider.Model, report.Provider.Runtime)
	builder.WriteString("## Automatic metrics\n\n")
	builder.WriteString("| Metric | Passed / checked | Rate |\n| --- | ---: | ---: |\n")
	for _, row := range []struct {
		name  string
		check Check
	}{
		{"valid output/schema", report.Summary.ValidOutput},
		{"Conventional Commit", report.Summary.Conventional},
		{"allowed scope", report.Summary.AllowedScope},
		{"subject length", report.Summary.SubjectLength},
		{"changed component", report.Summary.ChangedComponent},
		{"no unsupported component", report.Summary.UnsupportedComponent},
		{"no unsupported issue identifier", report.Summary.UnsupportedIssue},
		{"no unsupported test outcome", report.Summary.UnsupportedTestOutcome},
		{"no unsupported behavioral claim", report.Summary.UnsupportedBehavior},
		{"structural grounding coverage", report.Summary.StructuralGrounding},
	} {
		fmt.Fprintf(&builder, "| %s | %d / %d | %.2f%% |\n", row.name, row.check.Passed, row.check.Checked, row.check.Rate*100)
	}
	fmt.Fprintf(&builder, "\nCold generation: %.3f ms average; warm generation: %.3f ms average; approximate process allocation: %d bytes.\n\n", report.Summary.ColdLatencyMilliseconds, report.Summary.WarmLatencyMilliseconds, report.Summary.ApproxAllocatedBytes)
	builder.WriteString("## Per-case reference\n\n")
	builder.WriteString("Historical subjects are references for human review, not a string-similarity target. Raw diffs are intentionally omitted.\n\n")
	builder.WriteString("| Case | Reference subject | Candidates | Cold ms | Warm ms |\n| --- | --- | ---: | ---: | ---: |\n")
	for _, result := range report.Cases {
		fmt.Fprintf(&builder, "| %s | %s | %d | %.3f | %.3f |\n", escapeCell(result.ID), escapeCell(result.ReferenceSubject), result.CandidateCount, result.Generation.ColdMilliseconds, result.Generation.WarmMilliseconds)
	}
	fmt.Fprintf(&builder, "\n## Manual review required\n\n%s\n", report.ManualReview)
	return builder.String()
}

func ensurePrivateDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("evaluation report directory is not private: %s", directory)
	}
	return nil
}

func writePrivate(path string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".evaluation-")
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

func safeName(value string) string {
	value = strings.Map(func(character rune) rune {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			return character
		}
		return '-'
	}, strings.ToLower(value))
	return strings.Trim(value, "-")
}

func escapeCell(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "|", "\\|"), "\n", " ")
}
