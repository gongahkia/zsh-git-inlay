package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluationDirUsesPrivateStateOverride(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("ZSH_GIT_INLAY_STATE_DIR", state)
	directory, err := EvaluationDir()
	if err != nil {
		t.Fatal(err)
	}
	if directory != filepath.Join(state, "evaluations") {
		t.Fatalf("directory = %q", directory)
	}
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("private state directory: info=%v err=%v", info, err)
	}
}

func TestDataDirUsesPrivateOverride(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	t.Setenv("ZSH_GIT_INLAY_DATA_DIR", data)
	directory, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("private data directory: info=%v err=%v", info, err)
	}
}

func TestPrivateDirectoryRejectsSymlinkBeforeChangingPermissions(t *testing.T) {
	dir := t.TempDir()
	target, link := filepath.Join(dir, "target"), filepath.Join(dir, "link")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ensurePrivate(link); err == nil {
		t.Fatal("accepted symlink for private directory")
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("rejection changed symlink target permissions: %v", err)
	}
}

func TestRuntimeInspectionUsesTheSameFallbackWithoutCreatingIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZSH_GIT_INLAY_RUNTIME_DIR", "")
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "xdg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	if err := os.Mkdir(filepath.Join(dir, "xdg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "xdg/zsh-git-inlay"), []byte("unrelated file"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := RuntimePath()
	if err != nil || path != filepath.Join(dir, "cache/zsh-git-inlay/runtime") {
		t.Fatalf("inspection did not choose fallback: %q %v", path, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("inspection created fallback directory")
	}
	actual, err := Dir()
	if err != nil || actual != path {
		t.Fatalf("daemon disagrees with inspection: %q %v", actual, err)
	}
}
