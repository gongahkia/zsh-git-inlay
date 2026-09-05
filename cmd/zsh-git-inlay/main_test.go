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

	"github.com/gongahkia/zsh-git-inlay/internal/activity"
	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/daemon"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	"github.com/gongahkia/zsh-git-inlay/internal/learning"
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

func TestVersionCommandReportsEmbeddedBuildMetadata(t *testing.T) {
	output, err := captureCommandOutput(func() error { return run([]string{"version", "--json"}) })
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]string
	if err := json.Unmarshal([]byte(output), &report); err != nil || report["version"] == "" || report["commit"] == "" || report["go"] == "" {
		t.Fatalf("version report=%q decoded=%#v err=%v", output, report, err)
	}
}

func TestExplainCommandReportsGroundingForPreparedCandidate(t *testing.T) {
	repository := t.TempDir()
	runtimeDirectory, cacheDirectory := filepath.Join(t.TempDir(), "runtime"), filepath.Join(t.TempDir(), "cache")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ZSH_GIT_INLAY_RUNTIME_DIR", runtimeDirectory)
	t.Setenv("ZSH_GIT_INLAY_CACHE_DIR", cacheDirectory)
	t.Setenv("ZSH_GIT_INLAY_DATA_DIR", filepath.Join(t.TempDir(), "data"))
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
				if candidates, ok := report["candidates"].([]any); ok && len(candidates) > 0 && report["policy"] != nil && report["learning"] != nil {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("explain did not return grounding diagnostics for a prepared candidate")
}

func TestPermissionsCommandsPersistOnlyTheActivityGrant(t *testing.T) {
	dataDirectory := t.TempDir()
	t.Setenv("ZSH_GIT_INLAY_DATA_DIR", dataDirectory)
	output, err := captureCommandOutput(func() error { return run([]string{"permissions"}) })
	if err != nil || !strings.Contains(output, `"activity": false`) {
		t.Fatalf("default permissions output=%q err=%v", output, err)
	}
	output, err = captureCommandOutput(func() error { return run([]string{"permissions", "enable", "activity"}) })
	if err != nil || !strings.Contains(output, `"activity": true`) {
		t.Fatalf("enable output=%q err=%v", output, err)
	}
	path := activity.PermissionsPath(dataDirectory)
	info, statErr := os.Stat(path)
	if statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("permission file info=%v err=%v", info, statErr)
	}
	output, err = captureCommandOutput(func() error { return run([]string{"permissions", "revoke", "activity"}) })
	if err != nil || !strings.Contains(output, `"activity": false`) {
		t.Fatalf("revoke output=%q err=%v", output, err)
	}
	if err := run([]string{"permissions", "enable", "output"}); err == nil {
		t.Fatal("unsupported output permission was accepted")
	}
}

func TestCloudCommandsRequireConfirmationPreviewOnlyGrantedCategoriesAndPersistNoSource(t *testing.T) {
	dataDirectory := t.TempDir()
	t.Setenv("ZSH_GIT_INLAY_DATA_DIR", dataDirectory)
	repository := learningRepository(t)
	if err := os.WriteFile(filepath.Join(repository, "cloud.go"), []byte("package cloud\nconst token = \"ghp_not_exposed\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commandGit(t, repository, "add", "cloud.go")
	if err := run([]string{"cloud", "grant", "openai", "--classes", "staged_diff"}); err == nil {
		t.Fatal("cloud grant did not require confirmation")
	}
	withoutGrant, err := captureCommandOutput(func() error {
		return run([]string{"cloud", "preview", "--provider", "openai", "--cwd", repository, "--json"})
	})
	if err != nil || !strings.Contains(withoutGrant, "requires_explicit_grant") || strings.Contains(withoutGrant, "ghp_not_exposed") {
		t.Fatalf("ungranted cloud preview=%q err=%v", withoutGrant, err)
	}
	granted, err := captureCommandOutput(func() error {
		return run([]string{"cloud", "grant", "openai", "--classes", "staged_diff", "--confirm", "--json"})
	})
	if err != nil || !strings.Contains(granted, "staged_diff") || strings.Contains(granted, "ghp_not_exposed") {
		t.Fatalf("cloud grant=%q err=%v", granted, err)
	}
	preview, err := captureCommandOutput(func() error {
		return run([]string{"cloud", "preview", "--provider", "openai", "--cwd", repository, "--json"})
	})
	if err != nil || !strings.Contains(preview, "staged_patch") || strings.Contains(preview, "recent_subjects") || strings.Contains(preview, "ghp_not_exposed") {
		t.Fatalf("cloud preview=%q err=%v", preview, err)
	}
	content, err := os.ReadFile(filepath.Join(dataDirectory, "cloud-grants.json"))
	if err != nil || strings.Contains(string(content), "ghp_not_exposed") || strings.Contains(string(content), repository) {
		t.Fatalf("cloud grants=%q err=%v", content, err)
	}
	if err := run([]string{"cloud", "revoke", "openai"}); err != nil {
		t.Fatal(err)
	}
	status, err := captureCommandOutput(func() error { return run([]string{"cloud", "status", "--json"}) })
	if err != nil || !strings.Contains(status, `"providers": []`) {
		t.Fatalf("cloud status=%q err=%v", status, err)
	}
}

func TestComposeEditsGroundedBodyWithoutCommitting(t *testing.T) {
	repository := learningRepository(t)
	if err := os.WriteFile(filepath.Join(repository, "parser_test.go"), []byte("package parser\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commandGit(t, repository, "add", "parser_test.go")
	stop := startComposeDaemon(t, repository)
	defer stop()
	editor := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf '\\nUser-authored rationale.\\n' >> \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_EDITOR", editor)
	before := composeHead(t, repository)
	output, err := captureCommandOutput(func() error { return run([]string{"compose", "--cwd", repository, "--json"}) })
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("compose output=%q err=%v", output, err)
	}
	path, _ := report["output_path"].(string)
	if path == "" || report["committed"] != false || report["user_edited"] != true {
		t.Fatalf("compose report=%#v", report)
	}
	defer os.Remove(path)
	info, statErr := os.Stat(path)
	content, readErr := os.ReadFile(path)
	if statErr != nil || readErr != nil || info.Mode().Perm() != 0o600 || !strings.Contains(string(content), "Staged paths:") || !strings.Contains(string(content), "User-authored rationale.") {
		t.Fatalf("compose file info=%v stat=%v content=%q read=%v", info, statErr, content, readErr)
	}
	if after := composeHead(t, repository); after != before {
		t.Fatalf("compose created a commit: before=%s after=%s", before, after)
	}
}

func TestComposeRejectsStagedStateChangedInEditor(t *testing.T) {
	repository := learningRepository(t)
	if err := os.WriteFile(filepath.Join(repository, "parser_test.go"), []byte("package parser\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commandGit(t, repository, "add", "parser_test.go")
	stop := startComposeDaemon(t, repository)
	defer stop()
	editor := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf 'stale\\n' > \"$COMPOSE_REPOSITORY/stale.go\"\ngit -C \"$COMPOSE_REPOSITORY\" add stale.go\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_EDITOR", editor)
	t.Setenv("COMPOSE_REPOSITORY", repository)
	before := composeHead(t, repository)
	err := run([]string{"compose", "--cwd", repository, "--json"})
	if err == nil || !strings.Contains(err.Error(), "staged state changed during compose") {
		t.Fatalf("stale compose err=%v", err)
	}
	if marker := "remains at "; strings.Contains(err.Error(), marker) {
		defer os.Remove(strings.TrimSpace(strings.TrimPrefix(err.Error(), "staged state changed during compose; edited message was not used and remains at ")))
	}
	if after := composeHead(t, repository); after != before {
		t.Fatalf("stale compose created a commit: before=%s after=%s", before, after)
	}
}

func TestComposeSuppliesRequiredBodyWithoutNormalSuggestion(t *testing.T) {
	repository := learningRepository(t)
	if err := os.WriteFile(filepath.Join(repository, ".zsh-git-inlay.toml"), []byte("[commit]\nbody = \"required\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "parser_test.go"), []byte("package parser\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commandGit(t, repository, "add", "parser_test.go")
	stop := startComposeDaemon(t, repository)
	defer stop()
	output, err := captureCommandOutput(func() error {
		return run([]string{"suggest", "--cwd", repository, "--buffer", "git commit -m ", "--json", "--no-start"})
	})
	var suggestion map[string]string
	if json.Unmarshal([]byte(output), &suggestion) != nil || err != nil || suggestion["status"] != "body_required" {
		t.Fatalf("required-body suggestion output=%q err=%v", output, err)
	}
	editor := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_EDITOR", editor)
	output, err = captureCommandOutput(func() error { return run([]string{"compose", "--cwd", repository, "--json"}) })
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("compose output=%q err=%v", output, err)
	}
	path, _ := report["output_path"].(string)
	defer os.Remove(path)
	content, readErr := os.ReadFile(path)
	if path == "" || readErr != nil || !strings.Contains(string(content), "Staged paths:") || report["committed"] != false {
		t.Fatalf("required-body compose report=%#v content=%q err=%v", report, content, readErr)
	}
}

func TestActivityEmitCommandUsesConsentAndRedactsProducerData(t *testing.T) {
	repository := t.TempDir()
	runtimeDirectory, cacheDirectory, dataDirectory := filepath.Join(t.TempDir(), "runtime"), filepath.Join(t.TempDir(), "cache"), filepath.Join(t.TempDir(), "data")
	t.Setenv("ZSH_GIT_INLAY_RUNTIME_DIR", runtimeDirectory)
	t.Setenv("ZSH_GIT_INLAY_CACHE_DIR", cacheDirectory)
	t.Setenv("ZSH_GIT_INLAY_DATA_DIR", dataDirectory)
	commandGit(t, repository, "init", "-q", "-b", "main")
	commandGit(t, repository, "config", "user.name", "Test")
	commandGit(t, repository, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "file.go"), []byte("package file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commandGit(t, repository, "add", "file.go")
	if err := run([]string{"activity", "emit", "--cwd", repository, "--kind", "test.completed", "--data", "token=ghp_not_retained"}); err != nil {
		t.Fatalf("disabled emitter = %v", err)
	}
	if err := activity.SavePermissions(activity.PermissionsPath(dataDirectory), activity.Permissions{Activity: true}); err != nil {
		t.Fatal(err)
	}
	serverContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(serverContext, config.Default(), "") }()
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
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := call(ipc.Request{Version: ipc.Version, Operation: "status"}, 20*time.Millisecond); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := run([]string{"activity", "emit", "--cwd", repository, "--kind", "test.completed", "--data", "token=ghp_not_retained"}); err != nil {
		t.Fatalf("consented emitter = %v", err)
	}
	output, err := captureCommandOutput(func() error { return run([]string{"activity", "inspect", "--cwd", repository, "--json"}) })
	if err != nil || strings.Contains(output, "ghp_not_retained") || !strings.Contains(output, "[REDACTED]") || !strings.Contains(output, "test.completed") {
		t.Fatalf("activity inspection output=%q err=%v", output, err)
	}
}

func TestLearningCommandsAreScopedResettableAndCloneConfirmed(t *testing.T) {
	dataDirectory := t.TempDir()
	t.Setenv("ZSH_GIT_INLAY_DATA_DIR", dataDirectory)
	first, second := learningRepository(t), learningRepository(t)
	commandGit(t, first, "remote", "add", "origin", "https://name:token@example.invalid/owner/repository.git?private=true")
	commandGit(t, second, "remote", "add", "origin", "https://example.invalid/owner/repository.git")
	context, cancel := gitstate.WithTimeout()
	firstState, err := gitstate.Snapshot(context, first)
	cancel()
	if err != nil || firstState.Root == "" {
		t.Fatalf("first state=%#v err=%v", firstState, err)
	}
	store, err := learning.New(filepath.Join(dataDirectory, "learning"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Observe(firstState.RepoID, learning.Observation{Subject: "fix(api): update parser", Origin: learning.UserAuthored}); err != nil {
		t.Fatal(err)
	}
	status, err := captureCommandOutput(func() error { return run([]string{"learning", "status", "--cwd", first}) })
	if err != nil || !strings.Contains(status, "enabled: true") || !strings.Contains(status, "local_samples: 1") {
		t.Fatalf("learning status=%q err=%v", status, err)
	}
	inspection, err := captureCommandOutput(func() error { return run([]string{"learning", "inspect", "--cwd", first, "--json"}) })
	if err != nil || !strings.Contains(inspection, "historical_repository_prior") || strings.Contains(inspection, "establish base") {
		t.Fatalf("learning inspect=%q err=%v", inspection, err)
	}
	if _, err := captureCommandOutput(func() error { return run([]string{"learning", "disable", "--cwd", first}) }); err != nil {
		t.Fatal(err)
	}
	exported, err := captureCommandOutput(func() error { return run([]string{"learning", "export", "--cwd", first}) })
	if err != nil || strings.Contains(exported, "parser") {
		t.Fatalf("learning export=%q err=%v", exported, err)
	}
	exportPath := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(exportPath, []byte(exported), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureCommandOutput(func() error { return run([]string{"learning", "reset", "--cwd", first}) }); err != nil {
		t.Fatal(err)
	}
	reset, err := store.Load(firstState.RepoID)
	if err != nil || reset.Local.Samples != 0 || !reset.Enabled {
		t.Fatalf("reset=%#v err=%v", reset, err)
	}
	if _, err := captureCommandOutput(func() error { return run([]string{"learning", "import", "--cwd", first, "--file", exportPath}) }); err != nil {
		t.Fatal(err)
	}
	imported, err := store.Load(firstState.RepoID)
	if err != nil || imported.Local.Samples != 1 || imported.Enabled {
		t.Fatalf("imported=%#v err=%v", imported, err)
	}
	if err := run([]string{"learning", "clone-import", "--cwd", second, "--from", first}); err == nil {
		t.Fatal("matching clone import did not require confirmation")
	}
	if _, err := captureCommandOutput(func() error {
		return run([]string{"learning", "clone-import", "--cwd", second, "--from", first, "--confirm"})
	}); err != nil {
		t.Fatal(err)
	}
	context, cancel = gitstate.WithTimeout()
	secondState, err := gitstate.Snapshot(context, second)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	cloned, err := store.Load(secondState.RepoID)
	if err != nil || cloned.Repository == firstState.RepoID || cloned.Local.Samples != 1 {
		t.Fatalf("clone import=%#v err=%v", cloned, err)
	}
}

func learningRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	commandGit(t, repository, "init", "-q", "-b", "main")
	commandGit(t, repository, "config", "user.name", "Test")
	commandGit(t, repository, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "file.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commandGit(t, repository, "add", "file.txt")
	commandGit(t, repository, "commit", "-qm", "chore(repo): establish base")
	return repository
}

func startComposeDaemon(t *testing.T, repository string) func() {
	t.Helper()
	t.Setenv("ZSH_GIT_INLAY_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	t.Setenv("ZSH_GIT_INLAY_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("ZSH_GIT_INLAY_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	serverContext, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(serverContext, config.Default(), repository) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := captureCommandOutput(func() error { return run([]string{"candidates", "--cwd", repository, "--json"}) }); err == nil {
			return func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(time.Second):
					t.Error("compose daemon did not stop")
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	t.Fatal("compose daemon did not prepare candidates")
	return func() {}
}

func composeHead(t *testing.T, repository string) string {
	t.Helper()
	command := exec.Command("git", "-C", repository, "rev-parse", "HEAD")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
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
