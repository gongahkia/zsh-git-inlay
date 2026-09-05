// Package candidate contains the deterministic, metadata-only prototype provider.
package candidate

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"

	"os/exec"
)

const (
	MaxCandidates = 3
	MaxMessageLen = 160
	MaxMetadata   = 64 * 1024
)

type Candidate struct {
	Message string `json:"message"`
	Rank    int    `json:"rank"`
}

// Generate uses names and status codes only; it never reads staged file content.
func Generate(ctx context.Context, cwd string) ([]Candidate, error) {
	command := exec.CommandContext(ctx, "git", "-C", cwd, "diff", "--cached", "--name-status", "-z", "--find-renames")
	command.Env = []string{"GIT_OPTIONAL_LOCKS=0"}
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("read staged metadata: %w", err)
	}
	if len(output) > MaxMetadata {
		return nil, fmt.Errorf("staged metadata exceeds %d byte prototype limit", MaxMetadata)
	}
	changes := parseNameStatus(string(output))
	if len(changes) == 0 {
		return nil, fmt.Errorf("no staged metadata")
	}
	module := dominantModule(changes)
	kind := classify(changes)
	count := len(changes)
	var messages []string
	switch kind {
	case "docs":
		messages = []string{
			fmt.Sprintf("docs(%s): update documentation", module),
			fmt.Sprintf("docs(%s): refine staged documentation", module),
			fmt.Sprintf("docs(%s): refresh documentation changes", module),
		}
	case "test":
		messages = []string{
			fmt.Sprintf("test(%s): update test coverage", module),
			fmt.Sprintf("test(%s): refine staged tests", module),
			fmt.Sprintf("test(%s): prepare test updates", module),
		}
	case "build":
		messages = []string{
			fmt.Sprintf("build(%s): update dependencies", module),
			fmt.Sprintf("build(%s): refresh dependency metadata", module),
			fmt.Sprintf("build(%s): align staged dependencies", module),
		}
	default:
		messages = []string{
			fmt.Sprintf("chore(%s): update %d staged file%s", module, count, plural(count)),
			fmt.Sprintf("chore(%s): refine staged changes", module),
			fmt.Sprintf("chore(%s): prepare staged update", module),
		}
	}
	result := make([]Candidate, 0, MaxCandidates)
	for rank, message := range messages {
		if len(message) > MaxMessageLen || !safe(message) {
			return nil, fmt.Errorf("prototype generated unsafe candidate")
		}
		result = append(result, Candidate{Message: message, Rank: rank})
	}
	return result, nil
}

type change struct{ status, file string }

func parseNameStatus(raw string) []change {
	fields := strings.Split(raw, "\x00")
	changes := make([]change, 0, len(fields)/2)
	for index := 0; index+1 < len(fields); {
		status := fields[index]
		if status == "" {
			break
		}
		index++
		file := fields[index]
		index++
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if index >= len(fields) {
				break
			}
			file = fields[index]
			index++
		}
		changes = append(changes, change{status: status, file: file})
	}
	return changes
}

func dominantModule(changes []change) string {
	counts := map[string]int{}
	for _, change := range changes {
		part := strings.Split(strings.TrimPrefix(change.file, "./"), "/")[0]
		if part == "" || part == "." {
			part = path.Base(change.file)
		}
		counts[normalise(part)]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	best := "repo"
	for _, key := range keys {
		if counts[key] > counts[best] || best == "repo" {
			best = key
		}
	}
	return best
}

func classify(changes []change) string {
	all := func(match func(string) bool) bool {
		for _, change := range changes {
			if !match(strings.ToLower(change.file)) {
				return false
			}
		}
		return true
	}
	if all(func(file string) bool { return strings.HasSuffix(file, ".md") || strings.HasPrefix(file, "docs/") }) {
		return "docs"
	}
	if all(func(file string) bool { return strings.Contains(file, "test") || strings.HasSuffix(file, "_test.go") }) {
		return "test"
	}
	if all(func(file string) bool {
		base := path.Base(file)
		return base == "go.mod" || base == "go.sum" || base == "package.json" || base == "package-lock.json" || base == "cargo.toml" || base == "cargo.lock"
	}) {
		return "build"
	}
	return "chore"
}

func normalise(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(value) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '.' || character == '_' || character == '-' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('-')
		}
	}
	result := strings.Trim(builder.String(), "-.")
	if result == "" {
		return "repo"
	}
	if len(result) > 40 {
		return result[:40]
	}
	return result
}

func safe(value string) bool {
	if value == "" || len(value) > MaxMessageLen {
		return false
	}
	for _, character := range value {
		if !(unicode.IsLower(character) || unicode.IsDigit(character) || strings.ContainsRune(" ():-_./", character)) {
			return false
		}
	}
	return true
}

// Valid verifies an untrusted cached or provider-produced candidate before it
// can reach the shell composition layer.
func Valid(value Candidate) bool {
	return value.Rank >= 0 && value.Rank < MaxCandidates && safe(value.Message)
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
