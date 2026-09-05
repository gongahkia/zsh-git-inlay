package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/daemon"
)

func TestContextCommandReportsSummaryWithoutSourceContent(t *testing.T) {
	repository := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	commandGit(t, repository, "init", "-q", "-b", "feature/ABC-123-context")
	commandGit(t, repository, "config", "user.name", "Test")
	commandGit(t, repository, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "file.go"), []byte("package file\nconst api_key = \"CLI-SECRET-MUST-NOT-APPEAR\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commandGit(t, repository, "add", "file.go")
	output, err := captureCommandOutput(func() error {
		return run([]string{"context", "--cwd", repository, "--provider", "ollama", "--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "CLI-SECRET-MUST-NOT-APPEAR") {
		t.Fatalf("context command exposed source content: %s", output)
	}
	var summary map[string]any
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("invalid context JSON: %v\n%s", err, output)
	}
	if summary["provider"] != "ollama" || summary["staged_only"] != true || summary["context_fingerprint"] == "" || summary["sources"] == nil {
		t.Fatalf("unexpected context summary: %#v", summary)
	}
}

func TestExplainCommandReportsGroundingForPreparedCandidate(t *testing.T) {
	repository := t.TempDir()
	runtimeDirectory, cacheDirectory := filepath.Join(t.TempDir(), "runtime"), filepath.Join(t.TempDir(), "cache")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ZSH_GIT_INLAY_RUNTIME_DIR", runtimeDirectory)
	t.Setenv("ZSH_GIT_INLAY_CACHE_DIR", cacheDirectory)
	commandGit(t, repository, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repository, "parser_test.go"), []byte("package parser\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commandGit(t, repository, "add", "parser_test.go")
	serverContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(serverContext, config.Default(), repository) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("daemon did not stop")
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		output, err := captureCommandOutput(func() error {
			return run([]string{"explain", "--cwd", repository, "--json"})
		})
		if err == nil {
			var report map[string]any
			if json.Unmarshal([]byte(output), &report) == nil {
				if candidates, ok := report["candidates"].([]any); ok && len(candidates) > 0 {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("explain did not return grounding diagnostics for a prepared candidate")
}

func captureCommandOutput(run func() error) (string, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	previous := os.Stdout
	os.Stdout = writer
	runErr := run()
	_ = writer.Close()
	os.Stdout = previous
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if runErr != nil {
		return string(output), runErr
	}
	return string(output), readErr
}

func commandGit(t *testing.T, cwd string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
