package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/edit"
	runtimepath "github.com/gongahkia/zsh-git-inlay/internal/runtime"
	"github.com/gongahkia/zsh-git-inlay/internal/setup"
)

func uninstallCommand(arguments []string) error {
	flags, _, jsonOutput := commonFlags("uninstall")
	purge := flags.Bool("purge-local-data", false, "also remove product-owned preferences, feedback, models, caches and backups")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: zsh-git-inlay uninstall [--purge-local-data] [--json]")
	}
	manager, err := lifecycleManager()
	if err != nil {
		return err
	}
	var purgePaths []string
	if *purge {
		purgePaths, err = localPurgePaths()
		if err != nil {
			return err
		}
	}
	manifest, manifestErr := manager.Load()
	if manifestErr != nil && !os.IsNotExist(manifestErr) {
		return manifestErr
	}
	recovery := ""
	if manifestErr == nil && !*purge {
		recovery, err = prepareRecoveryHelper(manager.History.Dir)
		if err != nil {
			return err
		}
	}
	if err := stopForLifecycle(); err != nil {
		return err
	}
	result, err := manager.Uninstall()
	if err != nil {
		return err
	}
	if manifestErr == nil {
		// Remove empty owned directories only; unrelated prefix files survive.
		for _, path := range []string{filepath.Join(manifest.Prefix, "share/zsh-git-inlay"), filepath.Join(manifest.Prefix, "share"), filepath.Join(manifest.Prefix, "bin")} {
			_ = os.Remove(path)
		}
	}
	report := map[string]any{"changed": result.Changed, "preferences_retained": !*purge}
	if result.Journal.ID != "" {
		report["transaction"] = result.Journal.ID
		report["backup"] = filepath.Join(manager.History.Dir, result.Journal.ID+".json")
		report["recovery_helper"] = recovery
	}
	if *purge {
		for _, path := range purgePaths {
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("removed integration; could not purge %s: %w", path, err)
			}
		}
		delete(report, "transaction")
		delete(report, "backup")
		delete(report, "recovery_helper")
		report["purged"] = true
	}
	if *jsonOutput {
		return printJSON(report)
	}
	if *purge {
		fmt.Println("Removed integration and product-owned local data.")
		return nil
	}
	fmt.Println("Removed installed integration. Preferences, feedback and backups are retained.")
	if result.Journal.ID != "" {
		fmt.Printf("Backup: %s\nUndo: %s undo %s\n", report["backup"], setup.Quote(recovery), result.Journal.ID)
	}
	return nil
}

func prepareRecoveryHelper(historyDir string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	source, err := edit.Read(executable)
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(historyDir), "recovery", "zsh-git-inlay")
	// A retained private executable makes undo usable after removal from PATH.
	// It is removed together with backups by an explicit full purge.
	if err := edit.UpdatePrivateMode(path, 0o700, func([]byte) ([]byte, error) { return source.Data, nil }); err != nil {
		return "", err
	}
	for _, name := range []string{"THIRD_PARTY_NOTICES", "LICENSE"} {
		discovered := setup.Discover(executable)
		if discovered.Plugin == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(discovered.Plugin), name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := edit.UpdatePrivate(filepath.Join(filepath.Dir(path), name), func([]byte) ([]byte, error) { return data, nil }); err != nil {
			return "", err
		}
	}
	return path, nil
}

func localPurgePaths() ([]string, error) {
	data, err := runtimepath.DataPath()
	if err != nil {
		return nil, err
	}
	state, err := runtimepath.StateDir()
	if err != nil {
		return nil, err
	}
	cache, err := runtimepath.CacheDir()
	if err != nil {
		return nil, err
	}
	run, err := runtimepath.Dir()
	if err != nil {
		return nil, err
	}
	paths := []string{data, state, cache, run, filepath.Dir(config.Path())}
	roots := map[string]bool{}
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if filepath.Base(filepath.Dir(absolute)) == "zsh-git-inlay" {
			absolute = filepath.Dir(absolute)
		}
		if filepath.Base(absolute) != "zsh-git-inlay" {
			return nil, fmt.Errorf("refusing recursive purge of custom path %s; use a dedicated directory named zsh-git-inlay or remove its owned files explicitly", absolute)
		}
		info, err := os.Lstat(absolute)
		if err == nil {
			stat, owned := info.Sys().(*syscall.Stat_t)
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !owned || int(stat.Uid) != os.Getuid() {
				return nil, fmt.Errorf("refusing purge of unowned/non-directory/symlink %s", absolute)
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		roots[absolute] = true
	}
	result := make([]string, 0, len(roots))
	for path := range roots {
		result = append(result, path)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(result)))
	return result, nil
}
