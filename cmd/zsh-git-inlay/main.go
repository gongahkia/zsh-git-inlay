package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/command"
	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/daemon"
	"github.com/gongahkia/zsh-git-inlay/internal/evaluation"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/grounding"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	"github.com/gongahkia/zsh-git-inlay/internal/managed"
	"github.com/gongahkia/zsh-git-inlay/internal/provider"
	"github.com/gongahkia/zsh-git-inlay/internal/repoctx"
	runtimepath "github.com/gongahkia/zsh-git-inlay/internal/runtime"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "zsh-git-inlay:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return usage()
	}
	switch arguments[0] {
	case "doctor":
		return doctor(arguments[1:])
	case "config":
		return configCommand(arguments[1:])
	case "fingerprint":
		return fingerprint(arguments[1:])
	case "context":
		return contextCommand(arguments[1:])
	case "observe":
		return observe(arguments[1:])
	case "suggest":
		return suggest(arguments[1:])
	case "candidates":
		return candidates(arguments[1:])
	case "explain":
		return explain(arguments[1:])
	case "status":
		return status(arguments[1:])
	case "evaluate":
		return evaluateCommand(arguments[1:])
	case "model":
		return modelCommand(arguments[1:])
	case "daemon":
		return daemonCommand(arguments[1:])
	default:
		return usage()
	}
}

func usage() error {
	return errors.New("usage: zsh-git-inlay {doctor|config|status|fingerprint|context|observe|suggest|candidates|explain|evaluate|model|daemon serve|daemon stop}")
}

func commonFlags(name string) (*flag.FlagSet, *string, *bool) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	cwd := flags.String("cwd", "", "repository working directory")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	return flags, cwd, jsonOutput
}

func resolveCWD(value string) (string, error) {
	if value != "" {
		return filepath.Abs(value)
	}
	return os.Getwd()
}

func fingerprint(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("fingerprint")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return err
	}
	context, cancel := gitstate.WithTimeout()
	defer cancel()
	state, err := gitstate.Snapshot(context, cwd)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(state)
	}
	if state.Availability != gitstate.Ready {
		return fmt.Errorf("%s: %s", state.Availability, state.Reason)
	}
	fmt.Println(state.Fingerprint)
	return nil
}

func contextCommand(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("context")
	providerName := flags.String("provider", "deterministic", "provider preview budget")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return err
	}
	snapshotContext, cancel := gitstate.WithTimeout()
	defer cancel()
	state, err := gitstate.Snapshot(snapshotContext, cwd)
	if err != nil {
		return err
	}
	if state.Availability != gitstate.Ready {
		return fmt.Errorf("%s: %s", state.Availability, state.Reason)
	}
	compiled, err := repoctx.Compile(context.Background(), cwd, state, *providerName)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(compiled)
	}
	fmt.Printf("provider: %s\nstaged_only: %t\ncontext_fingerprint: %s\ntotal_bytes: %d/%d\n", compiled.Provider, compiled.StagedOnly, compiled.ContextFingerprint, compiled.TotalBytes, compiled.Budget.Total)
	for _, source := range compiled.Sources {
		status := "excluded"
		if source.Included {
			status = "included"
		}
		fmt.Printf("%s: %s %d/%d bytes — %s\n", source.Name, status, source.Bytes, source.Limit, source.Reason)
	}
	return nil
}

func observe(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("observe")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return err
	}
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: "observe", CWD: cwd}, 35*time.Millisecond)
	if err != nil {
		if err := ensureDaemon(cwd); err != nil {
			return err
		}
		if *jsonOutput {
			return printJSON(map[string]string{"status": "starting"})
		}
		fmt.Println("starting")
		return nil
	}
	if *jsonOutput {
		return printJSON(reply)
	}
	fmt.Println(reply.Status)
	return nil
}

func suggest(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("suggest")
	buffer := flags.String("buffer", "", "current ZLE buffer")
	cycle := flags.Int("cycle", 0, "candidate index")
	noStart := flags.Bool("no-start", false, "do not start a daemon")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if _, supported := command.Parse(*buffer, len(*buffer)); !supported {
		return suggestStatus(*jsonOutput, "unsupported_command")
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return suggestStatus(*jsonOutput, "outside_repository")
	}
	context, cancel := gitstate.WithTimeout()
	defer cancel()
	state, err := gitstate.Snapshot(context, cwd)
	if err != nil || state.Availability != gitstate.Ready {
		if err != nil {
			return suggestStatus(*jsonOutput, "error")
		}
		return suggestStatus(*jsonOutput, string(state.Availability))
	}
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID}, 20*time.Millisecond)
	if err != nil {
		if !*noStart {
			_ = ensureDaemon(cwd)
			return suggestStatus(*jsonOutput, "daemon_starting")
		}
		return suggestStatus(*jsonOutput, "daemon_unavailable")
	}
	if reply.Status != "ready" {
		return suggestStatus(*jsonOutput, reply.Status)
	}
	var record daemon.Record
	if json.Unmarshal(reply.Payload, &record) != nil {
		return suggestStatus(*jsonOutput, "malformed")
	}
	suggestion, ok := command.Suggestion(*buffer, len(*buffer), record.Candidates, *cycle)
	if ok && !*jsonOutput {
		fmt.Print(suggestion)
	}
	if !ok {
		return suggestStatus(*jsonOutput, "constrained_no_match")
	}
	if *jsonOutput {
		return printJSON(map[string]string{"status": "ready", "suggestion": suggestion})
	}
	return nil
}

func suggestStatus(jsonOutput bool, status string) error {
	if jsonOutput {
		return printJSON(map[string]string{"status": status})
	}
	return nil
}

func configCommand(arguments []string) error {
	if len(arguments) != 1 || arguments[0] != "cycle-keybinding" {
		return errors.New("usage: zsh-git-inlay config cycle-keybinding")
	}
	settings, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Println(settings.CycleKeybinding)
	return nil
}

func candidates(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("candidates")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return err
	}
	context, cancel := gitstate.WithTimeout()
	defer cancel()
	state, err := gitstate.Snapshot(context, cwd)
	if err != nil {
		return err
	}
	if state.Availability != gitstate.Ready {
		return fmt.Errorf("%s: %s", state.Availability, state.Reason)
	}
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID}, 100*time.Millisecond)
	if err != nil {
		return fmt.Errorf("daemon unavailable: %w", err)
	}
	if reply.Status != "ready" {
		return fmt.Errorf("candidates %s", reply.Status)
	}
	var record daemon.Record
	if err := json.Unmarshal(reply.Payload, &record); err != nil {
		return fmt.Errorf("decode candidates: %w", err)
	}
	if *jsonOutput {
		return printJSON(record)
	}
	for _, candidate := range record.Candidates {
		fmt.Println(candidate.Message)
	}
	return nil
}

func explain(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("explain")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return err
	}
	snapshotContext, cancel := gitstate.WithTimeout()
	defer cancel()
	state, err := gitstate.Snapshot(snapshotContext, cwd)
	if err != nil {
		return err
	}
	if state.Availability != gitstate.Ready {
		return fmt.Errorf("%s: %s", state.Availability, state.Reason)
	}
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID}, 100*time.Millisecond)
	if err != nil {
		return fmt.Errorf("daemon unavailable: %w", err)
	}
	if reply.Status != "ready" {
		return fmt.Errorf("explain %s", reply.Status)
	}
	var record daemon.Record
	if err := json.Unmarshal(reply.Payload, &record); err != nil {
		return fmt.Errorf("decode explanation: %w", err)
	}
	if len(record.Grounding) != len(record.Candidates) {
		return errors.New("grounding diagnostics are unavailable for this candidate record")
	}
	type explanation struct {
		Message   string           `json:"message"`
		Grounding grounding.Result `json:"grounding"`
	}
	values := make([]explanation, len(record.Candidates))
	for index, candidate := range record.Candidates {
		values[index] = explanation{Message: candidate.Message, Grounding: record.Grounding[index]}
	}
	if *jsonOutput {
		return printJSON(map[string]any{"fingerprint": record.Fingerprint, "provider": record.Provider, "policy": record.Policy, "candidates": values})
	}
	fmt.Printf("policy: %s (%s)\n", record.Policy.Source, record.Policy.Version)
	for _, value := range values {
		fmt.Printf("%s\n  %s score=%d\n", value.Message, value.Grounding.State, value.Grounding.Score)
		for _, check := range value.Grounding.Checks {
			outcome := "failed"
			if check.Passed {
				outcome = "passed"
			}
			kind := "heuristic"
			if check.Deterministic {
				kind = "deterministic"
			}
			fmt.Printf("  %s %s: %s\n", kind, check.Name, outcome)
		}
	}
	return nil
}

func status(arguments []string) error {
	flags, _, jsonOutput := commonFlags("status")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: "status"}, 100*time.Millisecond)
	if err != nil {
		if *jsonOutput {
			return printJSON(map[string]string{"status": "daemon_unavailable"})
		}
		fmt.Println("daemon unavailable")
		return nil
	}
	if *jsonOutput {
		fmt.Print(string(reply.Payload))
		fmt.Println()
		return nil
	}
	fmt.Println(string(reply.Payload))
	return nil
}

func evaluateCommand(arguments []string) error {
	flags := flag.NewFlagSet("evaluate", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	fixtures := flags.Bool("fixtures", false, "run the embedded synthetic corpus")
	repository := flags.String("repo", "", "replay eligible commits from this local repository")
	provider := flags.String("provider", "deterministic", "evaluation provider")
	timeout := flags.Duration("timeout", evaluation.DefaultOptions().Timeout, "per-generation timeout")
	limit := flags.Int("limit", evaluation.DefaultOptions().Limit, "maximum historical commits")
	outputDirectory := flags.String("output-dir", "", "private report directory")
	jsonOutput := flags.Bool("json", false, "emit report paths and summary as JSON")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if (*fixtures && *repository != "") || (!*fixtures && *repository == "") {
		return errors.New("usage: zsh-git-inlay evaluate --fixtures | --repo <local-repository>")
	}
	if *provider != "deterministic" {
		return fmt.Errorf("evaluation provider %q is unavailable; only deterministic is configured in this milestone", *provider)
	}
	options := evaluation.Options{Timeout: *timeout, Limit: *limit}
	var report evaluation.Report
	var err error
	if *fixtures {
		corpus, corpusErr := evaluation.LoadCorpus()
		if corpusErr != nil {
			return corpusErr
		}
		report, err = evaluation.EvaluateCorpus(context.Background(), corpus, evaluation.DeterministicGenerator{}, options)
	} else {
		report, err = evaluation.ReplayHistory(context.Background(), *repository, evaluation.DeterministicGenerator{}, options)
	}
	if err != nil {
		return err
	}
	directory := *outputDirectory
	if directory == "" {
		directory, err = runtimepath.EvaluationDir()
		if err != nil {
			return err
		}
	}
	paths, err := evaluation.WriteReports(directory, report)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(map[string]any{"reports": paths, "summary": report.Summary})
	}
	fmt.Printf("evaluation JSON: %s\nevaluation Markdown: %s\n", paths.JSON, paths.Markdown)
	return nil
}

func modelCommand(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: zsh-git-inlay model {status|install --confirm|rollback <version>|uninstall <version> --confirm}")
	}
	directory, err := runtimepath.DataDir()
	if err != nil {
		return err
	}
	manager, err := managed.New(directory)
	if err != nil {
		return err
	}
	switch arguments[0] {
	case "status":
		if len(arguments) != 1 {
			return errors.New("usage: zsh-git-inlay model status")
		}
		status, err := manager.Status()
		if err != nil {
			return err
		}
		return printJSON(status)
	case "install":
		if len(arguments) != 2 || arguments[1] != "--confirm" {
			return errors.New("managed model installation requires explicit --confirm")
		}
		return errors.New("no authenticated managed runtime/model manifest is bundled; no download was attempted")
	case "rollback":
		if len(arguments) != 2 {
			return errors.New("usage: zsh-git-inlay model rollback <version>")
		}
		return manager.Rollback(arguments[1])
	case "uninstall":
		if len(arguments) != 3 || arguments[2] != "--confirm" {
			return errors.New("usage: zsh-git-inlay model uninstall <version> --confirm")
		}
		return manager.Uninstall(arguments[1])
	default:
		return usage()
	}
}

func daemonCommand(arguments []string) error {
	if len(arguments) == 0 {
		return usage()
	}
	switch arguments[0] {
	case "serve":
		flags := flag.NewFlagSet("daemon serve", flag.ContinueOnError)
		flags.SetOutput(ioDiscard{})
		initial := flags.String("observe", "", "prepare this directory after startup")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		settings, err := config.Load()
		if err != nil {
			return err
		}
		return daemon.Serve(context.Background(), settings, *initial)
	case "stop":
		_, err := call(ipc.Request{Version: ipc.Version, Operation: "stop"}, 100*time.Millisecond)
		if err != nil {
			return fmt.Errorf("daemon unavailable: %w", err)
		}
		return nil
	default:
		return usage()
	}
}

func doctor(arguments []string) error {
	flags, _, jsonOutput := commonFlags("doctor")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	autosuggestions := false
	for _, path := range []string{
		"/usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh",
		"/usr/local/share/zsh-autosuggestions/zsh-autosuggestions.zsh",
	} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			autosuggestions = true
			break
		}
	}
	_, zshErr := exec.LookPath("zsh")
	_, gitErr := exec.LookPath("git")
	settings, settingsErr := config.Load()
	ollamaReport := map[string]any{"reachable": false, "models": []string{}}
	if ollama, err := provider.NewOllama(provider.DefaultOllamaURL, "diagnostic", 250*time.Millisecond); err == nil {
		if models, modelsErr := ollama.Models(context.Background()); modelsErr == nil {
			names := make([]string, 0, len(models))
			for _, model := range models {
				names = append(names, model.Name)
			}
			ollamaReport["reachable"], ollamaReport["models"] = true, names
		}
	}
	if settingsErr == nil && settings.Provider == "ollama" {
		ollamaReport["configured_model"] = settings.ProviderModel
		if ollamaReport["reachable"] == true {
			if ollama, err := provider.NewOllama(provider.DefaultOllamaURL, settings.ProviderModel, 250*time.Millisecond); err == nil {
				if model, showErr := ollama.Show(context.Background()); showErr == nil {
					ollamaReport["configured_model_details"] = model.Details
				}
			}
		}
	}
	managedReport := map[string]any{"available": false}
	if dataDir, dataErr := runtimepath.DataDir(); dataErr == nil {
		if manager, managerErr := managed.New(dataDir); managerErr == nil {
			if managedStatus, statusErr := manager.Status(); statusErr == nil {
				managedReport["available"], managedReport["status"] = true, managedStatus
			}
		}
	}
	socket, socketErr := runtimepath.SocketPath()
	state := "unavailable"
	if socketErr == nil {
		if _, err := call(ipc.Request{Version: ipc.Version, Operation: "status"}, 30*time.Millisecond); err == nil {
			state = "running"
		}
	}
	report := map[string]any{"zsh": zshErr == nil, "git": gitErr == nil, "zsh_autosuggestions_file": autosuggestions, "daemon": state, "runtime_socket": socket, "go": runtime.Version(), "ollama": ollamaReport, "managed_model": managedReport}
	if *jsonOutput {
		return printJSON(report)
	}
	for _, key := range []string{"zsh", "git", "zsh_autosuggestions_file", "daemon", "runtime_socket", "ollama", "managed_model", "go"} {
		fmt.Printf("%s: %v\n", key, report[key])
	}
	if !autosuggestions {
		fmt.Fprintln(os.Stdout, "action: install and source zsh-autosuggestions before zsh-git-inlay.plugin.zsh")
	}
	return nil
}

func ensureDaemon(cwd string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer devnull.Close()
	command := exec.Command(executable, "daemon", "serve", "--observe", cwd)
	command.Stdin = devnull
	command.Stdout = devnull
	command.Stderr = devnull
	if err := command.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	return command.Process.Release()
}

func call(request ipc.Request, timeout time.Duration) (ipc.Reply, error) {
	socket, err := runtimepath.SocketPath()
	if err != nil {
		return ipc.Reply{}, err
	}
	connection, err := net.DialTimeout("unix", socket, timeout)
	if err != nil {
		return ipc.Reply{}, err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(timeout))
	if err := ipc.WriteRequest(connection, request); err != nil {
		return ipc.Reply{}, err
	}
	reply, err := ipc.ReadReply(connection)
	if err != nil {
		return ipc.Reply{}, err
	}
	if reply.Status == "malformed" || reply.Status == "incompatible" {
		return reply, errors.New(reply.Error)
	}
	return reply, nil
}

func printJSON(value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

type ioDiscard struct{}

func (ioDiscard) Write(bytes []byte) (int, error) { return len(bytes), nil }

var _ = strings.Builder{}
