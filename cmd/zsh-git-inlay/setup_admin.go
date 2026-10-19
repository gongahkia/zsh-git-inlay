package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/daemon"
	"github.com/gongahkia/zsh-git-inlay/internal/edit"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	"github.com/gongahkia/zsh-git-inlay/internal/lifecycle"
	runtimepath "github.com/gongahkia/zsh-git-inlay/internal/runtime"
	"github.com/gongahkia/zsh-git-inlay/internal/setup"
)

type preferenceFlags map[string]string

func (values preferenceFlags) String() string { return "key=value" }
func (values preferenceFlags) Set(value string) error {
	key, raw, found := strings.Cut(value, "=")
	if !found || key == "" {
		return errors.New("--set expects key=value")
	}
	if _, exists := values[key]; exists {
		return fmt.Errorf("repeated --set for %s", key)
	}
	values[key] = raw
	return nil
}

func lifecycleManager() (lifecycle.Manager, error) {
	dir, err := runtimepath.DataDir()
	if err != nil {
		return lifecycle.Manager{}, err
	}
	store, err := editStore()
	return lifecycle.Manager{DataDir: dir, History: store}, err
}

func setupAdmin(operation string, arguments []string, input io.Reader, output io.Writer) error {
	flags, _, jsonOutput := commonFlags("setup " + operation)
	prefix := flags.String("prefix", "", "installation prefix (default ~/.local)")
	shellFile := flags.String("shell-file", "", "shell startup file (default actual ZDOTDIR/.zshrc)")
	dependency := flags.String("autosuggestions", "", "installed zsh-autosuggestions source file")
	source := flags.String("source", "", "built checkout or extracted release directory")
	noninteractive := flags.Bool("noninteractive", operation != "wizard", "apply explicit flags without prompts")
	repairConfig := flags.Bool("repair-config", operation == "repair", "quarantine invalid preferences before applying choices")
	repairFiles := flags.Bool("repair-files", operation == "repair", "restore modified manifest-owned files with backups")
	migrate := flags.Bool("migrate", false, "comment literal unmanaged plugin source lines before adding the block")
	adopt := flags.Bool("adopt", false, "back up and adopt existing untracked install artifacts")
	skipCheck := flags.Bool("skip-shell-check", false, "skip normal/login interactive startup probes")
	values := preferenceFlags{}
	flags.Var(values, "set", "typed configuration key=value; repeat for multiple choices")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected setup arguments; use --set key=value for preferences")
	}
	if *jsonOutput && !*noninteractive {
		return errors.New("use --noninteractive with --json, or run the interactive wizard without --json")
	}
	wizard := newWizard(input, output)
	if !*noninteractive {
		choice, err := wizard.ask("Action: install, customize, repair, disable, remove", "install", []string{"install", "customize", "repair", "disable", "remove"})
		if err != nil {
			return err
		}
		operation = choice
		if operation == "disable" {
			return controlCommand("disable", nil)
		}
		if operation == "remove" {
			return uninstallCommand(nil)
		}
		if operation == "repair" {
			*repairConfig, *repairFiles = true, true
		}
	}
	snapshot, err := edit.Read(config.Path())
	if err != nil {
		return err
	}
	content := snapshot.Data
	var repairs []config.Diagnostic
	if *repairConfig {
		repair, err := config.RepairDocument(content, snapshot.Path, false)
		if err != nil {
			return err
		}
		content, repairs = repair.Content, repair.Diagnostics
	}
	settings, err := config.Parse(content, snapshot.Path)
	if err != nil {
		return fmt.Errorf("preferences need repair; run config repair or setup repair: %w", err)
	}
	if !*noninteractive {
		if err := wizard.preferences(settings, values); err != nil {
			return err
		}
	}
	content, err = config.EditMany(content, snapshot.Path, values, false)
	if err != nil {
		return err
	}
	change := edit.Change{Before: snapshot, Data: content}
	if !*jsonOutput {
		for _, diagnostic := range repairs {
			fmt.Fprintln(output, "Repair:", diagnostic.Error())
		}
	}
	if operation == "customize" {
		if !*noninteractive {
			if err := wizard.review(snapshot.Path, "", values); err != nil {
				return err
			}
		}
		if bytes.Equal(snapshot.Data, content) {
			fmt.Fprintln(output, "Preferences already match.")
			return nil
		}
		store, err := editStore()
		if err != nil {
			return err
		}
		journal, err := store.Apply("setup customize", []edit.Change{change})
		if err != nil {
			return err
		}
		return printSetupChange(output, *jsonOutput, journal, store.Dir, map[string]any{"preferences": snapshot.Path, "repairs": repairs})
	}
	manager, err := lifecycleManager()
	if err != nil {
		return err
	}
	previous, previousErr := manager.Load()
	if previousErr != nil && !os.IsNotExist(previousErr) {
		return previousErr
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	}
	discovered := setup.Discover(binary)
	plugin := discovered.Plugin
	if *source != "" {
		root, err := filepath.Abs(*source)
		if err != nil {
			return err
		}
		binary = filepath.Join(root, ".build/zsh-git-inlay")
		if _, err := os.Stat(binary); err != nil {
			binary = filepath.Join(root, "zsh-git-inlay")
		}
		plugin = filepath.Join(root, "zsh-git-inlay.plugin.zsh")
	}
	if *dependency == "" {
		*dependency = discovered.Autosuggestions
	}
	if *dependency == "" && previousErr == nil {
		*dependency = previous.Autosuggestions
	}
	if *dependency == "" && !*noninteractive {
		*dependency, err = wizard.ask("Path to installed zsh-autosuggestions.zsh", "", nil)
		if err != nil {
			return err
		}
	}
	if *dependency == "" {
		return errors.New("install zsh-autosuggestions, then pass --autosuggestions /absolute/path/zsh-autosuggestions.zsh")
	}
	*dependency, err = filepath.Abs(*dependency)
	if err != nil {
		return err
	}
	if *prefix == "" {
		if previousErr == nil {
			*prefix = previous.Prefix
		}
	}
	if *prefix == "" {
		userDirectory, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		*prefix = filepath.Join(userDirectory, ".local")
	}
	*prefix, err = filepath.Abs(*prefix)
	if err != nil {
		return err
	}
	*shellFile, err = setup.ShellFile(context.Background(), *shellFile, os.Environ())
	if err != nil {
		return err
	}
	if plugin == "" {
		return errors.New("plugin source unavailable; pass --source <built-checkout-or-extracted-release>")
	}
	if !*noninteractive {
		fmt.Fprintf(output, "Install prefix: %s\nDependency: %s\n", *prefix, *dependency)
		if err := wizard.review(snapshot.Path, *shellFile, values); err != nil {
			return err
		}
	}
	notices := map[string]string{}
	for _, name := range []string{"LICENSE", "THIRD_PARTY_NOTICES"} {
		path := filepath.Join(filepath.Dir(plugin), name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			notices[name] = path
		}
	}
	options := lifecycle.Options{Prefix: *prefix, ShellFile: *shellFile, Autosuggestions: *dependency, Binary: binary, Plugin: plugin, Version: version + " (" + commit + ")", Notices: notices, MigrateLegacy: *migrate, Adopt: *adopt, Repair: *repairFiles, Configuration: &change}
	result, err := manager.Install(options)
	if err != nil {
		return err
	}
	report := map[string]any{"changed": result.Changed, "installation": result.Manifest, "repairs": repairs}
	var postErrors []error
	if result.Changed {
		if err := stopForLifecycle(); err != nil {
			postErrors = append(postErrors, err)
		}
	}
	if !*skipCheck {
		probes := []setup.ShellReport{}
		for _, login := range []bool{false, true} {
			probe, err := setup.Probe(context.Background(), login, os.Environ())
			probes = append(probes, probe)
			if err != nil {
				postErrors = append(postErrors, err)
				continue
			}
			if !probe.Loaded || !probe.DependencyLoaded || probe.PluginContract != setup.PluginContract || len(probe.Strategies) == 0 || probe.Strategies[0] != "git-inlay" || !probe.Async {
				postErrors = append(postErrors, fmt.Errorf("interactive shell (login=%t) did not activate the expected plugin, dependency, async mode and first strategy; run doctor --check", login))
			}
			if !sameFilePath(probe.Binary, filepath.Join(*prefix, "bin/zsh-git-inlay")) || !sameFilePath(probe.PluginSource, filepath.Join(*prefix, "share/zsh-git-inlay/zsh-git-inlay.plugin.zsh")) {
				postErrors = append(postErrors, fmt.Errorf("interactive shell (login=%t) loaded a different helper or plugin; inspect doctor --check", login))
			}
		}
		report["shell_checks"] = probes
	}
	if len(postErrors) > 0 {
		report["post_install_error"] = errors.Join(postErrors...).Error()
	}
	if err := printSetupChange(output, *jsonOutput, result.Journal, manager.History.Dir, report); err != nil {
		return err
	}
	if !*jsonOutput {
		fmt.Fprintln(output, "Open a new Zsh session to load your integration. Use config show or setup customize to tune preferences.")
	}
	return errors.Join(postErrors...)
}

func printSetupChange(output io.Writer, asJSON bool, journal edit.Journal, directory string, report map[string]any) error {
	if journal.ID != "" {
		report["changed"] = true
		report["transaction"], report["backup"] = journal.ID, filepath.Join(directory, journal.ID+".json")
	}
	if asJSON {
		return printJSON(report)
	}
	if journal.ID == "" {
		fmt.Fprintln(output, "Setup already matches.")
		return nil
	}
	fmt.Fprintf(output, "Updated setup.\nBackup: %s\nUndo: zsh-git-inlay undo %s\n", report["backup"], journal.ID)
	return nil
}

func sameFilePath(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	left, leftErr := filepath.EvalSymlinks(first)
	right, rightErr := filepath.EvalSymlinks(second)
	return leftErr == nil && rightErr == nil && left == right
}

func stopForLifecycle() error {
	socket, err := runtimepath.SocketPath()
	if err != nil {
		return err
	}
	if _, err := os.Lstat(socket); os.IsNotExist(err) {
		return nil
	}
	if _, err := call(ipc.Request{Version: ipc.Version, Operation: "stop"}, 200*time.Millisecond); err != nil {
		if changed, repairErr := daemon.RepairStaleSocket(socket); repairErr == nil {
			if _, statErr := os.Lstat(socket); changed || os.IsNotExist(statErr) {
				return nil
			}
		}
		return fmt.Errorf("restart the previous daemon with doctor --fix: %w", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(socket); os.IsNotExist(err) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("previous daemon did not remove its socket; run doctor --fix before retrying")
}
