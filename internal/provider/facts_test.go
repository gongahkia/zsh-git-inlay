package provider_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/daemon"
)

func factGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s %v", args, output, err)
	}
}

func TestFactualProviderUsesStagedChangesAndPreservesBaseline(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ZSH_GIT_INLAY_DATA_DIR", t.TempDir())
	root := t.TempDir()
	factGit(t, root, "init", "-q", "-b", "main")
	factGit(t, root, "config", "user.name", "Test")
	factGit(t, root, "config", "user.email", "test@example.invalid")
	manifest := filepath.Join(root, "package.json")
	before := `{
"dependencies": {
 "widget": "1.0.0"
}}`
	after := strings.ReplaceAll(before, "1.0.0", "2.0.0")
	if err := os.WriteFile(manifest, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	factGit(t, root, "add", ".")
	factGit(t, root, "commit", "-qm", "baseline")
	if err := os.WriteFile(manifest, []byte(after), 0600); err != nil {
		t.Fatal(err)
	}
	factGit(t, root, "add", ".")
	if err := os.WriteFile(manifest, []byte(strings.ReplaceAll(after, "2.0.0", "9.9.9")), 0600); err != nil {
		t.Fatal(err)
	}
	settings := config.Default()
	record, err := daemon.PrepareForEvaluation(context.Background(), settings, root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, value := range record.Candidates {
		if strings.Contains(value.Message, "widget from 1.0.0 to 2.0.0") {
			found = true
		}
		if strings.Contains(value.Message, "9.9.9") {
			t.Fatal("unstaged version used")
		}
	}
	if !found || !strings.Contains(record.Candidates[0].Message, "widget from 1.0.0 to 2.0.0") {
		t.Fatalf("dependency fact missing: %#v", record.Candidates)
	}
	settings.Provider = "deterministic-baseline"
	baseline, err := daemon.PrepareForEvaluation(context.Background(), settings, root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Provider.Model != "metadata-template" || baseline.Candidates[0].Message != "build(package.json): update dependencies" {
		t.Fatalf("baseline changed: %#v", baseline)
	}
}

func TestFactualProviderAddedDeclarationsAndDeletions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ZSH_GIT_INLAY_DATA_DIR", t.TempDir())
	root := t.TempDir()
	factGit(t, root, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "cache.go"), []byte("package cache\nfunc ClearCache() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	factGit(t, root, "add", ".")
	record, err := daemon.PrepareForEvaluation(context.Background(), config.Default(), root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, value := range record.Candidates {
		found = found || strings.Contains(value.Message, "ClearCache declaration")
	}
	if !found {
		t.Fatalf("declaration missing: %#v", record.Candidates)
	}
}

func TestFactualProviderNamesRenamesAndDeletions(t *testing.T) {
	for _, operation := range []string{"rename", "remove"} {
		t.Run(operation, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("ZSH_GIT_INLAY_DATA_DIR", t.TempDir())
			root := t.TempDir()
			factGit(t, root, "init", "-q", "-b", "main")
			factGit(t, root, "config", "user.name", "Test")
			factGit(t, root, "config", "user.email", "test@example.invalid")
			if err := os.WriteFile(filepath.Join(root, "OldName.go"), []byte("package example\n"), 0600); err != nil {
				t.Fatal(err)
			}
			factGit(t, root, "add", ".")
			factGit(t, root, "commit", "-qm", "baseline")
			expected := "remove OldName.go"
			if operation == "rename" {
				factGit(t, root, "mv", "OldName.go", "NewName.go")
				expected = "rename NewName.go"
			} else {
				factGit(t, root, "rm", "OldName.go")
			}
			record, err := daemon.PrepareForEvaluation(context.Background(), config.Default(), root, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(record.Candidates[0].Message, expected) {
				t.Fatalf("candidate=%#v", record.Candidates)
			}
			for _, value := range record.Candidates {
				if strings.Contains(value.Message, "refine staged") || strings.Contains(value.Message, "prepare staged") {
					t.Fatalf("generic padding: %s", value.Message)
				}
			}
		})
	}
}
