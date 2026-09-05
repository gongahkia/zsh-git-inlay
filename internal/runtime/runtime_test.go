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
