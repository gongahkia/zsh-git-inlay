package compose

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReadMessageRequiresPrivateRegularBoundedFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "message")
	if err := os.WriteFile(path, []byte("subject\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	message, err := ReadMessage(path)
	if err != nil || message != "subject\n\nbody\n" {
		t.Fatalf("message=%q err=%v", message, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMessage(path); err == nil {
		t.Fatal("group-readable compose file was accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "message-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMessage(link); err == nil {
		t.Fatal("symlinked compose file was accepted")
	}
}

func TestGitEditorRejectsShellExecutable(t *testing.T) {
	t.Setenv("GIT_EDITOR", "sh")
	if _, err := GitEditor(context.Background()); err == nil {
		t.Fatal("shell executable was accepted as an editor")
	}
}

func TestGitEditorRejectsShellSyntax(t *testing.T) {
	t.Setenv("GIT_EDITOR", "editor; unsafe")
	if _, err := GitEditor(context.Background()); err == nil {
		t.Fatal("shell editor syntax was accepted")
	}
}

func TestGitEditorIgnoresRepositoryLocalSetting(t *testing.T) {
	repository := t.TempDir()
	command := exec.Command("git", "-C", repository, "init", "-q")
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	command = exec.Command("git", "-C", repository, "config", "core.editor", "/bin/true")
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_EDITOR", "")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "global"))
	t.Setenv("VISUAL", "sh")
	if _, err := GitEditor(context.Background()); err == nil {
		t.Fatal("repository-local core.editor was used")
	}
}
