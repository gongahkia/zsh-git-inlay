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
