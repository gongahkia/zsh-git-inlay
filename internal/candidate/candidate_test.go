package candidate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGenerateIsDeterministicAndMetadataOnly(t *testing.T) {
	repository := candidateRepository(t)
	writeCandidate(t, repository, "docs/guide.md", "untrusted $(not executed) filename content\n")
	gitCandidate(t, repository, "add", ".")
	first, err := Generate(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != MaxCandidates {
		t.Fatalf("candidates = %#v", first)
	}
	if first[0].Message != "docs(docs): update documentation" {
		t.Fatalf("primary = %q", first[0].Message)
	}
}

func TestGenerateClassifiesTestsAndDependencies(t *testing.T) {
	for _, test := range []struct{ name, file, want string }{
		{"tests", "internal/thing_test.go", "test(internal): update test coverage"},
		{"dependencies", "go.mod", "build(go.mod): update dependencies"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := candidateRepository(t)
			writeCandidate(t, repository, test.file, "x\n")
			gitCandidate(t, repository, "add", ".")
			got, err := Generate(context.Background(), repository)
			if err != nil {
				t.Fatal(err)
			}
			if got[0].Message != test.want {
				t.Fatalf("got %q, want %q", got[0].Message, test.want)
			}
		})
	}
}

func candidateRepository(t *testing.T) string {
	repository := t.TempDir()
	gitCandidate(t, repository, "init", "-q", "-b", "main")
	return repository
}
func writeCandidate(t *testing.T, root, name, value string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}
func gitCandidate(t *testing.T, cwd string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
