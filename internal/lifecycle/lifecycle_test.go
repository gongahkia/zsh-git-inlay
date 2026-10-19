package lifecycle

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/zsh-git-inlay/internal/edit"
)

func fixture(t *testing.T) (Manager, Options) {
	t.Helper()
	dir := t.TempDir()
	options := Options{Prefix: filepath.Join(dir, "prefix with ' quotes"), ShellFile: filepath.Join(dir, ".zshrc"), Autosuggestions: filepath.Join(dir, "autosuggestions.zsh"), Binary: filepath.Join(dir, "source-binary"), Plugin: filepath.Join(dir, "source-plugin"), Version: "test-1"}
	for path, content := range map[string]string{options.Binary: "binary-v1", options.Plugin: "# plugin-v1\n", options.Autosuggestions: "# dependency\n", options.ShellFile: "# my settings\nexport MY_SETTING=original\n"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return Manager{DataDir: filepath.Join(dir, "data"), History: edit.Store{Dir: filepath.Join(dir, "state/history")}}, options
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInstallRepeatUpgradeRemoveAndUndoWithSymlinkDotfile(t *testing.T) {
	manager, options := fixture(t)
	target := options.ShellFile + "-dotfiles-target"
	if err := os.Rename(options.ShellFile, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, options.ShellFile); err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Install(options)
	if err != nil || !installed.Changed {
		t.Fatalf("install: %+v %v", installed, err)
	}
	if info, err := os.Lstat(options.ShellFile); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("shell link replaced: %v", err)
	}
	if info, err := os.Stat(filepath.Join(options.Prefix, "bin/zsh-git-inlay")); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed binary permissions: %v", err)
	}
	if repeated, err := manager.Install(options); err != nil || repeated.Changed {
		t.Fatalf("repeat installation changed files: %+v %v", repeated, err)
	}
	if err := os.WriteFile(options.Binary, []byte("binary-v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	options.Version = "test-2"
	upgraded, err := manager.Install(options)
	if err != nil || !upgraded.Changed {
		t.Fatalf("upgrade: %v", err)
	}
	if _, err := manager.History.UndoWith(upgraded.Journal.ID, MergeUndo); err != nil {
		t.Fatal(err)
	}
	if string(readFile(t, filepath.Join(options.Prefix, "bin/zsh-git-inlay"))) != "binary-v1" {
		t.Fatal("undo did not restore old binary")
	}
	file, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("# my later edit\n")
	if closeErr := file.Close(); err != nil || closeErr != nil {
		t.Fatalf("append: %v %v", err, closeErr)
	}
	if _, err := manager.History.UndoWith(installed.Journal.ID, MergeUndo); err != nil {
		t.Fatal(err)
	}
	data := readFile(t, target)
	if !bytes.Contains(data, []byte("# my later edit\n")) || bytes.Contains(data, []byte(BlockStart)) || !bytes.Contains(data, []byte("export MY_SETTING=original\n")) {
		t.Fatalf("undo lost later edits or owned block remains: %s", data)
	}
	if _, err := os.Stat(filepath.Join(options.Prefix, "bin/zsh-git-inlay")); !os.IsNotExist(err) {
		t.Fatalf("undo left installed binary: %v", err)
	}
	if _, err := manager.History.UndoWith(installed.Journal.ID, MergeUndo); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(options); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manager.DataDir, "preferences-sentinel"), []byte("retain"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := manager.Uninstall()
	if err != nil || !removed.Changed {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(manager.DataDir, "preferences-sentinel")); err != nil {
		t.Fatal("default uninstall removed user data")
	}
	if repeated, err := manager.Uninstall(); err != nil || repeated.Changed {
		t.Fatalf("repeat uninstall: %+v %v", repeated, err)
	}
}

func TestUninstallPreservesModifiedArtifactsAndUnrelatedFiles(t *testing.T) {
	manager, options := fixture(t)
	if _, err := manager.Install(options); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(options.Prefix, "share/zsh-git-inlay/zsh-git-inlay.plugin.zsh")
	if err := os.WriteFile(plugin, []byte("# user modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Uninstall(); err == nil || !strings.Contains(err.Error(), "preserve modified") {
		t.Fatalf("modified artifact removed: %v", err)
	}
	if !bytes.Contains(readFile(t, options.ShellFile), []byte(BlockStart)) {
		t.Fatal("failed uninstall partly removed integration")
	}
	if string(readFile(t, plugin)) != "# user modified\n" {
		t.Fatal("failed uninstall discarded changes")
	}
	options.Repair = true
	if _, err := manager.Install(options); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Uninstall(); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyMigrationAndMalformedBlocksRequireExplicitAction(t *testing.T) {
	manager, options := fixture(t)
	legacy := "# user\nsource '/old/zsh-git-inlay.plugin.zsh'\n# other settings\n"
	if err := os.WriteFile(options.ShellFile, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(options); err == nil {
		t.Fatal("unmanaged source was overwritten silently")
	}
	if string(readFile(t, options.ShellFile)) != legacy {
		t.Fatal("failed plan modified shell file")
	}
	options.MigrateLegacy = true
	installed, err := manager.Install(options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(readFile(t, options.ShellFile), []byte("# zsh-git-inlay migrated: source '/old/")) {
		t.Fatal("migration discarded original source line")
	}
	if _, err := manager.History.Undo(installed.Journal.ID); err != nil {
		t.Fatal(err)
	}
	if string(readFile(t, options.ShellFile)) != legacy {
		t.Fatal("undo did not restore legacy source line")
	}
	for _, broken := range []string{BlockStart + "\n", BlockEnd + "\n", BlockStart + "\n" + BlockStart + "\n" + BlockEnd + "\n", string(Block(options.Prefix, options.Autosuggestions)) + string(Block(options.Prefix, options.Autosuggestions))} {
		if _, err := ReplaceBlock([]byte(broken), Block(options.Prefix, options.Autosuggestions), false); err == nil {
			t.Fatalf("accepted ambiguous block: %s", broken)
		}
	}
}

func TestUndoRejectsChangesInsideManagedRegion(t *testing.T) {
	manager, options := fixture(t)
	installed, err := manager.Install(options)
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Replace(readFile(t, options.ShellFile), []byte("path=("), []byte("# user change\npath=("), 1)
	if err := os.WriteFile(options.ShellFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.History.UndoWith(installed.Journal.ID, MergeUndo); err == nil {
		t.Fatal("undo discarded a user edit inside the owned region")
	}
	if !bytes.Equal(readFile(t, options.ShellFile), data) {
		t.Fatal("failed undo changed shell config")
	}
}

func TestUninstallNeverDeletesASymlinkArtifactTarget(t *testing.T) {
	manager, options := fixture(t)
	if _, err := manager.Install(options); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(options.Prefix, "bin/zsh-git-inlay")
	external := filepath.Join(filepath.Dir(options.Binary), "external-binary")
	if err := os.Rename(binary, external); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, binary); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Uninstall(); err == nil {
		t.Fatal("uninstall followed artifact symlink")
	}
	if string(readFile(t, external)) != "binary-v1" {
		t.Fatal("uninstall changed symlink target")
	}
}

func TestSetupAndPreferenceChangeAreOneValidatedUndoableTransaction(t *testing.T) {
	manager, options := fixture(t)
	path := filepath.Join(filepath.Dir(options.Binary), "preferences.toml")
	before, err := edit.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	options.Configuration = &edit.Change{Before: before, Data: []byte("[suggestions]\ncandidate_count = 0\n")}
	if _, err := manager.Install(options); err == nil {
		t.Fatal("invalid wizard configuration passed installation")
	}
	if _, err := os.Stat(filepath.Join(options.Prefix, "bin/zsh-git-inlay")); !os.IsNotExist(err) {
		t.Fatal("invalid wizard configuration partly installed files")
	}
	options.Configuration.Data = []byte("# preferences\n[commit]\ncapitalization = 'sentence'\n")
	installed, err := manager.Install(options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(readFile(t, path), []byte("sentence")) {
		t.Fatal("wizard preferences missing")
	}
	if _, err := manager.History.UndoWith(installed.Journal.ID, MergeUndo); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("undo left wizard preferences")
	}
}
