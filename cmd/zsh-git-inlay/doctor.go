package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/cloud"
	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/control"
	"github.com/gongahkia/zsh-git-inlay/internal/daemon"
	"github.com/gongahkia/zsh-git-inlay/internal/edit"
	"github.com/gongahkia/zsh-git-inlay/internal/feedback"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	"github.com/gongahkia/zsh-git-inlay/internal/lifecycle"
	"github.com/gongahkia/zsh-git-inlay/internal/provider"
	runtimepath "github.com/gongahkia/zsh-git-inlay/internal/runtime"
	"github.com/gongahkia/zsh-git-inlay/internal/setup"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Action string `json:"action,omitempty"`
}

func doctorCommand(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("doctor")
	check := flags.Bool("check", false, "also load actual normal/login interactive startup files")
	fix := flags.Bool("fix", false, "back up and repair owned config/integration; also check actual startup")
	shellFile := flags.String("shell-file", "", "explicit shell file for inspection")
	dependency := flags.String("autosuggestions", "", "explicit dependency entrypoint for the clean-shell check")
	for _, arg := range arguments {
		if arg == "--help" || arg == "-h" {
			fmt.Println("usage: zsh-git-inlay doctor [--check] [--fix] [--json] [--cwd directory] [--shell-file path] [--autosuggestions path]\nDefault checks inspect configuration, owned files, controls, permissions and daemon state.\n--check verifies fresh normal/login interactive shells; --fix backs up conservative repairs and verifies their result.")
			return nil
		}
	}
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected doctor arguments; use doctor --help")
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return err
	}
	repairs := []doctorRepair{}
	var fixErr error
	if *fix {
		repairs, fixErr = doctorFix(cwd)
		*check = true
	}
	checks := []doctorCheck{}
	add := func(name, status, detail, action string) {
		checks = append(checks, doctorCheck{name, status, detail, action})
	}
	if fixErr != nil {
		add("repair", "fail", fixErr.Error(), "Inspect the reported conflict and its private backup; later user edits have been preserved.")
	}
	binary, _ := os.Executable()
	installation := setup.Discover(binary)
	if *dependency != "" {
		installation.Autosuggestions = *dependency
	}
	settings, settingsErr := config.Load()
	installation.ConfigValid = settingsErr == nil
	if settingsErr != nil {
		installation.Problem = settingsErr.Error()
		add("user_config", "fail", settingsErr.Error(), "Run zsh-git-inlay config repair, or set the reported key to a valid value.")
	} else {
		add("user_config", "pass", "Valid typed preferences: "+config.Path(), "")
	}
	for _, item := range []struct {
		name    string
		present bool
		action  string
	}{{"git", installation.Git, "Install Git and make it available on PATH."}, {"zsh", installation.Zsh, "Install Zsh and make it available on PATH."}, {"plugin_file", installation.Plugin != "", "Use setup install --source <built checkout or extracted release>."}} {
		status, detail := "pass", "Available"
		if !item.present {
			status, detail = "fail", "Required component is unavailable"
		}
		add(item.name, status, detail, item.action)
	}
	dataPath, dataErr := runtimepath.DataPath()
	statePath, stateErr := runtimepath.StatePath()
	cachePath, cacheErr := runtimepath.CachePath()
	runPath, runErr := runtimepath.RuntimePath()
	for _, item := range []struct {
		name, path string
		err        error
	}{{"data_directory", dataPath, dataErr}, {"state_directory", statePath, stateErr}, {"cache_directory", cachePath, cacheErr}, {"runtime_directory", runPath, runErr}} {
		if item.err != nil {
			add(item.name, "fail", item.err.Error(), "Correct the corresponding XDG/ZSH_GIT_INLAY directory override.")
			continue
		}
		if err := inspectPrivateDirectory(item.path); err != nil {
			add(item.name, "fail", err.Error(), "Use doctor --fix for owned directory permissions; preserve and correct a symlink/unowned path explicitly.")
		} else {
			add(item.name, "pass", item.path+" (private, or not yet created)", "")
		}
	}
	manager := lifecycle.Manager{DataDir: dataPath, History: edit.Store{Dir: filepath.Join(statePath, "history")}}
	manifest, manifestErr := manager.Load()
	if manifestErr == nil {
		problems := manager.ArtifactProblems(manifest)
		if len(problems) > 0 {
			add("managed_artifacts", "fail", errors.Join(problems...).Error(), "Run doctor --fix to restore manifest-verified backups, or reinstall an explicitly selected trusted source.")
		} else {
			add("managed_artifacts", "pass", "Installed files match their recorded hashes, modes and targets", "")
		}
		if *shellFile == "" {
			*shellFile = manifest.ShellFile
		}
		if installation.Autosuggestions == "" {
			installation.Autosuggestions = manifest.Autosuggestions
		}
	} else if !os.IsNotExist(manifestErr) {
		add("installation_manifest", "fail", manifestErr.Error(), "Restore the private installation manifest from its backup; no untracked files will be removed.")
	} else {
		add("installation_manifest", "warning", "No managed installation is recorded; manual shell integration can still work", "Use setup --wizard if you want managed upgrades, repair and removal.")
	}
	if *shellFile == "" {
		if *check && installation.Zsh {
			*shellFile, err = setup.ShellFile(context.Background(), "", os.Environ())
		} else {
			base := os.Getenv("ZDOTDIR")
			if base == "" {
				base, _ = os.UserHomeDir()
			}
			*shellFile = filepath.Join(base, ".zshrc")
		}
		if err != nil {
			add("shell_location", "fail", err.Error(), "Pass --shell-file explicitly after checking your .zshenv/ZDOTDIR settings.")
		}
	}
	if *shellFile != "" {
		shell, readErr := edit.Read(*shellFile)
		if readErr != nil {
			add("shell_file", "fail", readErr.Error(), "Correct the reported shell-file path or permissions.")
		} else {
			if manifestErr == nil {
				if err := lifecycle.VerifyBlock(shell.Data, manifest); err != nil {
					add("managed_block", "fail", err.Error()+": "+*shellFile, "Run doctor --fix for an unambiguous owned block; inspect malformed/duplicate markers manually.")
				} else {
					add("managed_block", "pass", "Exactly one recorded integration block", "")
				}
			}
			if sourceInfo, err := os.Stat(*shellFile); err == nil {
				if compiled, err := os.Stat(*shellFile + ".zwc"); err == nil && compiled.ModTime().After(sourceInfo.ModTime()) {
					add("compiled_startup", "warning", "A newer compiled startup file can shadow edits: "+*shellFile+".zwc", "Recompile the shell file or touch its source after reviewing it; doctor preserves your compiled artifact.")
				}
			}
		}
	}
	if headers, err := manager.History.Inspect(); err != nil {
		add("undo_history", "fail", err.Error(), "Inspect the reported journal; preserve backups before repairing the directory or file.")
	} else {
		pending := []string{}
		for _, header := range headers {
			if header.State == "prepared" || header.State == "undoing" {
				pending = append(pending, header.ID)
			}
		}
		if len(pending) > 0 {
			add("undo_history", "fail", "Interrupted operations: "+strings.Join(pending, ", "), "Run doctor --fix to recover; later user edits cause a reported conflict rather than being overwritten.")
		} else {
			add("undo_history", "pass", fmt.Sprintf("%d recoverable operation records; no interrupted write", len(headers)), "")
		}
	}
	if settingsErr == nil {
		if root, err := doctorRepositoryRoot(cwd); err == nil {
			if _, err := config.LoadRepositoryPolicy(root); err != nil {
				add("repository_config", "fail", err.Error(), "Run config repair --repo --cwd <repository>, or doctor --fix in this repository.")
			} else {
				add("repository_config", "pass", "Valid repository policy with user preferences beneath overrides", "")
			}
		}
	}
	if path, err := control.Path(); err != nil {
		add("controls", "fail", err.Error(), "Correct local data directory settings.")
	} else if decision, err := control.Check(path, ""); err != nil {
		add("controls", "fail", err.Error(), "Run doctor --fix; corrupt controls are backed up and replaced with global disable until you explicitly enable.")
	} else {
		status := "pass"
		if decision.Status != "enabled" {
			status = "warning"
		}
		add("controls", status, "Global suggestions: "+decision.Status, "Use control status --repo, enable, or enable --repo to inspect and change the effective scope.")
	}
	if os.Getenv("ZSH_GIT_INLAY_DISABLED") == "1" {
		add("session_control", "warning", "ZSH_GIT_INLAY_DISABLED=1 suppresses this session", "Unset ZSH_GIT_INLAY_DISABLED in this session when you want suggestions again.")
	}
	if dataErr == nil {
		if _, err := feedback.Load(filepath.Join(dataPath, "feedback.json")); err != nil {
			add("feedback", "warning", err.Error(), "Move the reported feedback file aside before starting fresh, or restore its known-good copy.")
		} else {
			add("feedback", "pass", "Local explicit feedback is readable; no commit acceptance is inferred from these labels", "")
		}
	}
	probes := []setup.ShellReport{}
	if *check && installation.Zsh {
		for _, login := range []bool{false, true} {
			probe, err := setup.Probe(context.Background(), login, os.Environ())
			probes = append(probes, probe)
			name := "interactive_shell"
			if login {
				name = "login_interactive_shell"
			}
			if err != nil {
				add(name, "fail", err.Error(), "Correct the startup failure or timeout shown in startup_diagnostics, then retry doctor --check.")
				continue
			}
			if !probe.Loaded || !probe.DependencyLoaded || len(probe.Strategies) == 0 || probe.Strategies[0] != "git-inlay" {
				detail := fmt.Sprintf("dependency_loaded=%t, plugin_loaded=%t, strategy_order=%s", probe.DependencyLoaded, probe.Loaded, strings.Join(probe.Strategies, ","))
				if probe.Stderr != "" {
					detail += "; startup: " + probe.Stderr
				}
				add(name, "fail", detail, "Source zsh-autosuggestions before the plugin; use setup --wizard or doctor --fix for managed integration.")
				continue
			}
			add(name, "pass", "Fresh shell activated dependency and git-inlay first", "")
			if probe.PluginContract != setup.PluginContract {
				add(name+"_contract", "fail", "Loaded plugin contract "+probe.PluginContract+"; helper expects "+setup.PluginContract, "Upgrade the plugin/helper together with setup upgrade.")
			} else {
				add(name+"_contract", "pass", "Plugin contract "+probe.PluginContract, "")
			}
			if !probe.Async {
				add(name+"_async", "fail", "zsh-autosuggestions async mode is disabled", "Set ZSH_AUTOSUGGEST_USE_ASYNC=1 before sourcing zsh-autosuggestions; review any later unset in your shell configuration.")
			} else {
				add(name+"_async", "pass", "Autosuggestions are fetched asynchronously", "")
			}
			if !sameFileDigest(probe.Binary, binary) {
				add(name+"_binary", "fail", "The helper on the shell PATH differs from this running helper: "+probe.Binary, "Run doctor with the helper from your shell PATH, or upgrade the recorded installation.")
			} else {
				add(name+"_binary", "pass", probe.Binary, "")
			}
			if manifestErr == nil && !sameFileDigest(probe.PluginSource, filepath.Join(manifest.Prefix, "share/zsh-git-inlay/zsh-git-inlay.plugin.zsh")) {
				add(name+"_plugin_source", "fail", "The active plugin differs from the recorded installation: "+probe.PluginSource, "Remove the untracked source line or migrate it explicitly with setup --wizard --migrate.")
			}
			if settingsErr == nil && probe.CycleBinding != "none" {
				for _, keymap := range probe.CycleKeymaps {
					if !strings.HasSuffix(probe.Bindings[keymap], "zsh-git-inlay-cycle") {
						add(name+"_binding_"+keymap, "fail", "Configured cycle binding is absent or overwritten: "+probe.CycleBinding, "Choose a binding/keymap with config set; inspect later bindkey commands in your shell file.")
					}
					previous := probe.PreviousBindings[keymap]
					if previous != "" && previous != "undefined-key" && previous != "zsh-git-inlay-cycle" {
						add(name+"_conflict_"+keymap, "warning", "Cycling replaced existing widget "+previous+" on "+probe.CycleBinding, "Choose another zsh.cycle_keybinding or set it to none; unloading restores the previous widget.")
					}
				}
			}
			if probe.Stderr != "" {
				add(name+"_startup_diagnostics", "warning", probe.Stderr, "Review startup output; shell activation checks above report whether the plugin itself is working.")
			}
		}
	} else if !*check {
		add("shell_activation", "warning", "Actual startup has not been executed by this read-only inspection", "Run doctor --check to verify fresh normal/login interactive shells.")
	}
	if installation.Autosuggestions == "" {
		active := false
		for _, probe := range probes {
			active = active || probe.DependencyLoaded
		}
		if active {
			add("dependency_file", "warning", "Loaded by your shell, but its complete entrypoint was not discovered", "Pass --autosuggestions <entrypoint> when adopting the integration into the wizard.")
		} else {
			add("dependency_file", "fail", "zsh-autosuggestions source is unavailable", "Install zsh-autosuggestions or set ZSH_AUTOSUGGESTIONS_SOURCE to its readable entrypoint.")
		}
	} else {
		info, err := os.Stat(installation.Autosuggestions)
		if err != nil || !info.Mode().IsRegular() {
			add("dependency_file", "fail", "Dependency entrypoint is not a readable regular file: "+installation.Autosuggestions, "Correct --autosuggestions or ZSH_AUTOSUGGESTIONS_SOURCE.")
		} else {
			add("dependency_file", "pass", installation.Autosuggestions, "")
		}
	}
	ollamaReport := map[string]any{"reachable": false, "models": []string{}}
	if settingsErr == nil {
		if settings.Provider == "ollama" {
			level := "fail"
			if settings.ProviderFallback == "deterministic" {
				level = "warning"
			}
			client, clientErr := provider.NewOllama(provider.DefaultOllamaURL, settings.ProviderModel, 250*time.Millisecond)
			if clientErr != nil {
				add("provider", level, clientErr.Error(), "Correct provider settings with setup customize.")
			} else if models, err := client.Models(context.Background()); err != nil {
				add("provider", level, "Ollama is unavailable: "+err.Error(), "Start your local Ollama runtime and install the selected model, or select deterministic in setup customize.")
			} else {
				ollamaReport["reachable"] = true
				names := []string{}
				found := false
				for _, model := range models {
					names = append(names, model.Name)
					found = found || strings.TrimSuffix(model.Name, ":latest") == strings.TrimSuffix(settings.ProviderModel, ":latest")
				}
				ollamaReport["models"] = names
				if !found {
					add("provider", level, "Configured Ollama model is not installed: "+settings.ProviderModel, "Install that model explicitly or select an installed model with config set provider.model.")
				} else {
					add("provider", "pass", "Local Ollama model is installed: "+settings.ProviderModel, "")
				}
			}
		} else if settings.Provider == "openai" && dataErr == nil {
			store, _ := cloud.New(dataPath)
			grant, err := store.Grant("openai")
			if err != nil || len(grant.Classes) == 0 {
				add("cloud_consent", "fail", "Explicit OpenAI context grant is missing or invalid", "Review cloud inspect, then issue an explicit cloud grant for the context classes you want to share.")
			} else {
				add("cloud_consent", "pass", "Explicit OpenAI context grant exists", "")
			}
			if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == "" {
				add("cloud_credential", "fail", "OPENAI_API_KEY is unavailable; its value is never printed", "Set the credential in your environment, or select a local provider.")
			} else {
				add("cloud_credential", "pass", "Credential is present (value withheld)", "")
			}
		} else {
			add("provider", "pass", "Selected local provider: "+settings.Provider, "")
		}
	}
	socket, socketErr := runtimeSocketForDoctor()
	socketState := "unavailable"
	var daemonStatus daemon.Status
	if socketErr != nil {
		add("daemon", "fail", socketErr.Error(), "Correct the runtime directory override.")
	} else {
		socketState, err = daemon.InspectSocket(socket)
		if err != nil {
			add("daemon", "fail", err.Error(), "Use doctor --fix for a verified stale socket; move an unowned/non-socket path aside explicitly.")
		} else {
			switch socketState {
			case "missing":
				add("daemon", "pass", "Stopped; preparation starts on the next prompt/observe", "")
			case "stale":
				add("daemon", "warning", "A refused owned socket remains after shutdown", "Run doctor --fix; normal daemon startup also safely repairs this socket.")
			case "running":
				reply, err := callSocket(socket, ipc.Request{Version: ipc.Version, Operation: "status"}, 200*time.Millisecond)
				if err != nil {
					add("daemon", "fail", err.Error(), "Stop the previous daemon with its installed helper and retry setup upgrade.")
				} else if err := json.Unmarshal(reply.Payload, &daemonStatus); err != nil {
					add("daemon", "fail", "Malformed daemon status: "+err.Error(), "Stop and restart the product daemon; doctor never kills arbitrary PIDs.")
				} else {
					current := daemon.CurrentBuild(version, commit)
					if daemonStatus.Build.SHA256 == "" || daemonStatus.Build.SHA256 != current.SHA256 || daemonStatus.Build.Protocol != ipc.Version {
						add("daemon_build", "fail", "Running daemon differs from the current helper or cannot report its build", "Run doctor --fix to stop the old product daemon; the next prompt starts the current build.")
					} else {
						add("daemon_build", "pass", daemonStatus.Build.Version+" ("+daemonStatus.Build.Commit+")", "")
					}
				}
			}
		}
	}
	failures := []error{}
	for _, item := range checks {
		if item.Status == "fail" {
			failures = append(failures, fmt.Errorf("%s: %s", item.Name, item.Detail))
		}
	}
	installation.CheckPassed = *check && len(failures) == 0
	if len(failures) > 0 && installation.Problem == "" {
		installation.Problem = failures[0].Error()
	}
	report := map[string]any{"ok": len(failures) == 0, "checks": checks, "repairs": repairs, "installation": installation, "shell_checks": probes, "build": daemon.CurrentBuild(version, commit), "daemon": socketState, "daemon_status": daemonStatus, "runtime_socket": socket, "zsh": installation.Zsh, "git": installation.Git, "zsh_autosuggestions_file": installation.Autosuggestions != "", "ollama": ollamaReport, "go": runtime.Version()}
	if *jsonOutput {
		if err := printJSON(report); err != nil {
			return err
		}
	} else {
		for _, item := range checks {
			fmt.Printf("%s %s: %s\n", strings.ToUpper(item.Status), item.Name, item.Detail)
			if item.Status != "pass" && item.Action != "" {
				fmt.Println("  Action:", item.Action)
			}
		}
		for _, repair := range repairs {
			fmt.Println("REPAIRED", repair.Kind+":", repair.Detail)
			if repair.Transaction != "" {
				fmt.Printf("  Backup: %s\n  Undo: zsh-git-inlay undo %s\n", repair.Backup, repair.Transaction)
			}
		}
	}
	return errors.Join(failures...)
}

func runtimeSocketForDoctor() (string, error) {
	path, err := runtimepath.RuntimePath()
	return filepath.Join(path, "daemon.sock"), err
}

func inspectPrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	stat, owned := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 || !owned || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("%s must be a private user-owned directory, not a symlink", path)
	}
	return nil
}

func sameFileDigest(first, second string) bool {
	if sameFilePath(first, second) {
		return true
	}
	left, leftErr := doctorFileDigest(first)
	right, rightErr := doctorFileDigest(second)
	return leftErr == nil && rightErr == nil && left == right
}

func doctorFileDigest(path string) ([32]byte, error) {
	var empty [32]byte
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return empty, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return empty, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return empty, errors.New("component must be a regular file of at most 64 MiB")
	}
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(file, (64<<20)+1))
	if err != nil {
		return empty, err
	}
	if count != info.Size() {
		return empty, errors.New("component changed during inspection")
	}
	copy(empty[:], hash.Sum(nil))
	return empty, nil
}
