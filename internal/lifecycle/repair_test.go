package lifecycle

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRepairRestoresVerifiedArtifactsAndOwnedBlockWithUndo(t *testing.T) {
	manager, options := fixture(t)
	if _, err := manager.Install(options); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(options.Prefix, "share/zsh-git-inlay/zsh-git-inlay.plugin.zsh")
	if err := os.WriteFile(plugin, []byte("# corrupted installed plugin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(options.ShellFile, []byte("# user's new shell settings\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := manager.Load()
	if err != nil || len(manager.ArtifactProblems(manifest)) != 1 {
		t.Fatalf("artifact drift was hidden: %v", err)
	}
	repaired, err := manager.Repair()
	if err != nil || !repaired.Changed {
		t.Fatalf("repair: %v", err)
	}
	if string(readFile(t, plugin)) != "# plugin-v1\n" {
		t.Fatal("repair trusted the corrupted installed plugin")
	}
	data := readFile(t, options.ShellFile)
	if !bytes.HasPrefix(data, []byte("# user's new shell settings\n")) || VerifyBlock(data, manifest) != nil {
		t.Fatal("repair lost user settings or did not restore owned block")
	}
	if _, err := manager.History.Undo(repaired.Journal.ID); err != nil {
		t.Fatal(err)
	}
	if string(readFile(t, plugin)) != "# corrupted installed plugin\n" || string(readFile(t, options.ShellFile)) != "# user's new shell settings\n" {
		t.Fatal("repair backup did not preserve the original broken files")
	}
}
