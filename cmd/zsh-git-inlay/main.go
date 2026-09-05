package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/activity"
	"github.com/gongahkia/zsh-git-inlay/internal/cloud"
	"github.com/gongahkia/zsh-git-inlay/internal/command"
	"github.com/gongahkia/zsh-git-inlay/internal/compose"
	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/daemon"
	"github.com/gongahkia/zsh-git-inlay/internal/evaluation"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/grounding"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	"github.com/gongahkia/zsh-git-inlay/internal/learning"
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
	case "permissions":
		return permissionsCommand(arguments[1:])
	case "cloud":
		return cloudCommand(arguments[1:])
	case "compose":
		return composeCommand(arguments[1:])
	case "activity":
		return activityCommand(arguments[1:])
	case "learning":
		return learningCommand(arguments[1:])
	case "daemon":
		return daemonCommand(arguments[1:])
	default:
		return usage()
	}
}

func usage() error {
	return errors.New("usage: zsh-git-inlay {doctor|config|status|fingerprint|context|observe|suggest|candidates|explain|evaluate|model|permissions|cloud|compose|activity|learning|daemon serve|daemon stop}")
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
	if record.Policy.Body == "required" {
		return suggestStatus(*jsonOutput, "body_required")
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
		Message   string              `json:"message"`
		Grounding grounding.Result    `json:"grounding"`
		Learning  learning.Adjustment `json:"learning,omitempty"`
	}
	values := make([]explanation, len(record.Candidates))
	for index, candidate := range record.Candidates {
		value := explanation{Message: candidate.Message, Grounding: record.Grounding[index]}
		if len(record.Learning.Adjustments) == len(record.Candidates) {
			value.Learning = record.Learning.Adjustments[index]
		}
		values[index] = value
	}
	if *jsonOutput {
		return printJSON(map[string]any{"fingerprint": record.Fingerprint, "provider": record.Provider, "policy": record.Policy, "learning": record.Learning, "candidates": values})
	}
	fmt.Printf("policy: %s (%s)\n", record.Policy.Source, record.Policy.Version)
	for _, value := range values {
		fmt.Printf("%s\n  %s score=%d learning=%d\n", value.Message, value.Grounding.State, value.Grounding.Score, value.Learning.Score)
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

func permissionsCommand(arguments []string) error {
	directory, err := runtimepath.DataDir()
	if err != nil {
		return err
	}
	path := activity.PermissionsPath(directory)
	permissions, err := activity.LoadPermissions(path)
	if err != nil && !(len(arguments) == 2 && arguments[0] == "revoke" && arguments[1] == "activity") {
		return err
	}
	if err != nil {
		permissions = activity.Permissions{}
	}
	if len(arguments) == 0 {
		return printJSON(permissions)
	}
	if len(arguments) != 2 || arguments[1] != "activity" || (arguments[0] != "enable" && arguments[0] != "revoke") {
		return errors.New("usage: zsh-git-inlay permissions {enable activity|revoke activity}")
	}
	permissions.Activity = arguments[0] == "enable"
	if err := activity.SavePermissions(path, permissions); err != nil {
		return err
	}
	return printJSON(permissions)
}

func cloudCommand(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: zsh-git-inlay cloud {status|preview|grant|revoke}")
	}
	switch arguments[0] {
	case "status":
		return cloudStatus(arguments[1:])
	case "preview":
		return cloudPreview(arguments[1:])
	case "grant":
		return cloudGrant(arguments[1:])
	case "revoke":
		return cloudRevoke(arguments[1:])
	default:
		return errors.New("usage: zsh-git-inlay cloud {status|preview|grant|revoke}")
	}
}

// compose is a secondary editor workflow. It only writes a user-owned message
// file and never invokes git commit or changes the index.
func composeCommand(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("compose")
	candidateIndex := flags.Int("candidate", 0, "prepared candidate index")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *candidateIndex < 0 {
		return errors.New("usage: zsh-git-inlay compose [--cwd <directory>] [--candidate <index>] [--json]")
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return err
	}
	snapshotContext, cancel := gitstate.WithTimeout()
	state, err := gitstate.Snapshot(snapshotContext, cwd)
	cancel()
	if err != nil {
		return err
	}
	if state.Availability != gitstate.Ready {
		return fmt.Errorf("%s: %s", state.Availability, state.Reason)
	}
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID}, 100*time.Millisecond)
	if err != nil {
		return fmt.Errorf("compose requires prepared candidates: %w", err)
	}
	if reply.Status != "ready" {
		return fmt.Errorf("compose requires prepared candidates: %s", reply.Status)
	}
	var record daemon.Record
	if err := json.Unmarshal(reply.Payload, &record); err != nil {
		return fmt.Errorf("decode compose candidates: %w", err)
	}
	if record.Fingerprint != state.Fingerprint || *candidateIndex >= len(record.Candidates) || len(record.Grounding) != len(record.Candidates) {
		return errors.New("compose candidate record is unavailable")
	}
	if !grounding.EligibleForBodyComposition(record.Grounding[*candidateIndex]) {
		return errors.New("compose requires a grounded prepared candidate")
	}
	compiled, err := repoctx.Compile(context.Background(), cwd, state, record.Provider.Name)
	if err != nil {
		return err
	}
	plan, err := compose.Propose(record.Candidates[*candidateIndex].Message, compiled.Evidence(), record.Policy)
	if err != nil {
		return err
	}
	editor, err := compose.GitEditor(context.Background())
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "zsh-git-inlay-compose-")
	if err != nil {
		return err
	}
	path := file.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err == nil {
		_, err = file.WriteString(plan.Message())
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := editor.Run(context.Background(), path); err != nil {
		keep = true
		return fmt.Errorf("compose editor failed; edited message remains at %s", path)
	}
	edited, err := compose.ReadMessage(path)
	if err != nil {
		keep = true
		return fmt.Errorf("compose could not read the edited message; it remains at %s", path)
	}
	verifyContext, verifyCancel := gitstate.WithTimeout()
	current, snapshotErr := gitstate.Snapshot(verifyContext, cwd)
	verifyCancel()
	if snapshotErr != nil || current.Availability != gitstate.Ready || current.Fingerprint != state.Fingerprint {
		keep = true
		return fmt.Errorf("staged state changed during compose; edited message was not used and remains at %s", path)
	}
	if err := compose.ValidateEdited(edited, record.Policy); err != nil {
		keep = true
		return fmt.Errorf("edited message does not satisfy repository policy; it remains at %s", path)
	}
	keep = true
	report := map[string]any{"fingerprint": state.Fingerprint, "candidate": record.Candidates[*candidateIndex].Message, "output_path": path, "proposed_body_grounding": plan.Grounding, "user_edited": edited != plan.Message(), "committed": false}
	if *jsonOutput {
		return printJSON(report)
	}
	fmt.Printf("composition verified; no commit was created\nmessage_file: %s\n", path)
	return nil
}

func cloudStore() (*cloud.Store, error) {
	directory, err := runtimepath.DataDir()
	if err != nil {
		return nil, err
	}
	return cloud.New(directory)
}

func cloudStatus(arguments []string) error {
	flags := flag.NewFlagSet("cloud status", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return errors.New("usage: zsh-git-inlay cloud status [--json]")
	}
	store, err := cloudStore()
	if err != nil {
		return err
	}
	grants, err := store.Status()
	if err != nil {
		return err
	}
	report := map[string]any{"providers": grants, "available_context_classes": cloud.Classes()}
	if *jsonOutput {
		return printJSON(report)
	}
	for _, grant := range grants {
		fmt.Printf("provider: %s\ncontext_classes: %s\n", grant.Provider, joinCloudClasses(grant.Classes))
	}
	if len(grants) == 0 {
		fmt.Println("no cloud context is granted")
	}
	return nil
}

func cloudGrant(arguments []string) error {
	if len(arguments) == 0 || !cloud.ValidProvider(arguments[0]) {
		return errors.New("usage: zsh-git-inlay cloud grant openai --classes <comma-separated-classes> --confirm [--json]")
	}
	providerName := arguments[0]
	flags := flag.NewFlagSet("cloud grant", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	classesValue := flags.String("classes", "", "comma-separated context classes")
	confirm := flags.Bool("confirm", false, "confirm complete replacement of this provider grant")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 || !*confirm || *classesValue == "" {
		return errors.New("usage: zsh-git-inlay cloud grant openai --classes <comma-separated-classes> --confirm [--json]")
	}
	classes := make([]cloud.ContextClass, 0)
	for _, value := range strings.Split(*classesValue, ",") {
		classes = append(classes, cloud.ContextClass(strings.TrimSpace(value)))
	}
	store, err := cloudStore()
	if err != nil {
		return err
	}
	grant, err := store.Set(providerName, classes)
	if err != nil {
		return err
	}
	notifyCloudChanged(providerName)
	if *jsonOutput {
		return printJSON(grant)
	}
	fmt.Printf("provider: %s\ncontext_classes: %s\n", grant.Provider, joinCloudClasses(grant.Classes))
	return nil
}

func cloudRevoke(arguments []string) error {
	if len(arguments) != 1 || !cloud.ValidProvider(arguments[0]) {
		return errors.New("usage: zsh-git-inlay cloud revoke openai")
	}
	store, err := cloudStore()
	if err != nil {
		return err
	}
	if err := store.Revoke(arguments[0]); err != nil {
		return err
	}
	notifyCloudChanged(arguments[0])
	fmt.Printf("revoked cloud context for %s\n", arguments[0])
	return nil
}

func cloudPreview(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("cloud preview")
	providerName := flags.String("provider", "", "cloud provider")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || !cloud.ValidProvider(*providerName) {
		return errors.New("usage: zsh-git-inlay cloud preview --provider openai [--cwd <directory>] [--json]")
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
	store, err := cloudStore()
	if err != nil {
		return err
	}
	grant, err := store.Grant(*providerName)
	if err != nil {
		return err
	}
	report := map[string]any{"provider": *providerName, "granted_context_classes": grant.Classes, "requires_explicit_grant": len(grant.Classes) == 0, "sources": []repoctx.Source{}, "total_bytes": 0}
	if len(grant.Classes) != 0 {
		selected, err := compiled.SelectCloud(*providerName, grant.Classes)
		if err != nil {
			return err
		}
		report["context_fingerprint"] = selected.ContextFingerprint
		report["sources"] = selected.Sources
		report["total_bytes"] = selected.TotalBytes
	}
	if *jsonOutput {
		return printJSON(report)
	}
	fmt.Printf("provider: %s\ngranted_context_classes: %s\n", *providerName, joinCloudClasses(grant.Classes))
	if len(grant.Classes) == 0 {
		fmt.Println("would_send: nothing; an explicit grant is required")
		return nil
	}
	for _, value := range report["sources"].([]repoctx.Source) {
		fmt.Printf("would_send: %s %d/%d bytes — %s\n", value.Name, value.Bytes, value.Limit, value.Reason)
	}
	if len(report["sources"].([]repoctx.Source)) == 0 {
		fmt.Println("would_send: no currently available source for the granted classes")
	}
	return nil
}

func joinCloudClasses(classes []cloud.ContextClass) string {
	values := make([]string, len(classes))
	for index, class := range classes {
		values[index] = string(class)
	}
	return strings.Join(values, ",")
}

func notifyCloudChanged(providerName string) {
	_, _ = call(ipc.Request{Version: ipc.Version, Operation: "cloud_changed", Provider: providerName}, 100*time.Millisecond)
}

func learningCommand(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: zsh-git-inlay learning {status|inspect|reset|disable|enable|export|import|clone-import}")
	}
	switch arguments[0] {
	case "status", "inspect":
		return learningInspect(arguments)
	case "reset", "disable", "enable":
		return learningMutate(arguments)
	case "export":
		return learningExport(arguments[1:])
	case "import":
		return learningImport(arguments[1:])
	case "clone-import":
		return learningCloneImport(arguments[1:])
	case "prepare", "commit":
		return learningHook(arguments[0], arguments[1:])
	default:
		return errors.New("usage: zsh-git-inlay learning {status|inspect|reset|disable|enable|export|import|clone-import}")
	}
}

func learningInspect(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("learning " + arguments[0])
	if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 {
		return errors.New("usage: zsh-git-inlay learning " + arguments[0] + " [--cwd <directory>] [--json]")
	}
	cwd, state, store, err := learningScope(*cwdFlag)
	if err != nil {
		return err
	}
	profile, err := store.Load(state.RepoID)
	if err != nil {
		return err
	}
	if arguments[0] == "status" {
		if *jsonOutput {
			return printJSON(map[string]any{"enabled": profile.Enabled, "profile_version": profile.Version, "local_samples": profile.Local.Samples})
		}
		fmt.Printf("enabled: %t\nprofile_version: %d\nlocal_samples: %d\n", profile.Enabled, profile.Version, profile.Local.Samples)
		return nil
	}
	context, cancel := gitstate.WithTimeout()
	historical, historyErr := learning.Historical(context, cwd)
	cancel()
	if historyErr != nil {
		return fmt.Errorf("inspect historical repository prior: %w", historyErr)
	}
	inspection := map[string]any{"profile": profile, "historical_repository_prior": historical}
	if *jsonOutput {
		return printJSON(inspection)
	}
	fmt.Printf("enabled: %t\nprofile_version: %d\nlocal_samples: %d\nhistorical_samples: %d\n", profile.Enabled, profile.Version, profile.Local.Samples, historical.Samples)
	return nil
}

func learningMutate(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("learning " + arguments[0])
	if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 {
		return errors.New("usage: zsh-git-inlay learning " + arguments[0] + " [--cwd <directory>] [--json]")
	}
	cwd, state, store, err := learningScope(*cwdFlag)
	if err != nil {
		return err
	}
	var profile learning.Profile
	switch arguments[0] {
	case "reset":
		if err := store.Reset(state.RepoID); err != nil {
			return err
		}
		profile, err = store.Load(state.RepoID)
	case "disable":
		profile, err = store.SetEnabled(state.RepoID, false)
	case "enable":
		profile, err = store.SetEnabled(state.RepoID, true)
	}
	if err != nil {
		return err
	}
	notifyLearningChanged(cwd)
	if *jsonOutput {
		return printJSON(profile)
	}
	fmt.Printf("enabled: %t\nprofile_version: %d\nlocal_samples: %d\n", profile.Enabled, profile.Version, profile.Local.Samples)
	return nil
}

func learningExport(arguments []string) error {
	flags, cwdFlag, _ := commonFlags("learning export")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return errors.New("usage: zsh-git-inlay learning export [--cwd <directory>]")
	}
	_, state, store, err := learningScope(*cwdFlag)
	if err != nil {
		return err
	}
	exported, err := store.Export(state.RepoID)
	if err != nil {
		return err
	}
	return printJSON(exported)
}

func learningImport(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("learning import")
	filePath := flags.String("file", "", "exported learning profile")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *filePath == "" {
		return errors.New("usage: zsh-git-inlay learning import --file <path> [--cwd <directory>] [--json]")
	}
	cwd, state, store, err := learningScope(*cwdFlag)
	if err != nil {
		return err
	}
	exported, err := readLearningExport(*filePath)
	if err != nil {
		return err
	}
	profile, err := store.Import(state.RepoID, exported)
	if err != nil {
		return err
	}
	notifyLearningChanged(cwd)
	if *jsonOutput {
		return printJSON(profile)
	}
	fmt.Printf("enabled: %t\nprofile_version: %d\nlocal_samples: %d\n", profile.Enabled, profile.Version, profile.Local.Samples)
	return nil
}

func learningCloneImport(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("learning clone-import")
	from := flags.String("from", "", "local clone to import from")
	confirm := flags.Bool("confirm", false, "confirm matching-clone profile import")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *from == "" {
		return errors.New("usage: zsh-git-inlay learning clone-import --from <directory> --confirm [--cwd <directory>] [--json]")
	}
	cwd, target, store, err := learningScope(*cwdFlag)
	if err != nil {
		return err
	}
	sourceDirectory, err := filepath.Abs(*from)
	if err != nil {
		return err
	}
	context, cancel := gitstate.WithTimeout()
	source, err := gitstate.Snapshot(context, sourceDirectory)
	cancel()
	if err != nil || source.Root == "" || source.RepoID == "" {
		return errors.New("source is not a Git worktree")
	}
	context, cancel = gitstate.WithTimeout()
	targetRemote, targetErr := learning.RemoteHash(context, cwd)
	cancel()
	context, cancel = gitstate.WithTimeout()
	sourceRemote, sourceErr := learning.RemoteHash(context, source.Root)
	cancel()
	if targetErr != nil || sourceErr != nil || targetRemote != sourceRemote {
		return errors.New("source clone does not match this repository's normalized origin")
	}
	if !*confirm {
		return errors.New("matching clone profile import requires explicit --confirm")
	}
	exported, err := store.Export(source.RepoID)
	if err != nil {
		return err
	}
	profile, err := store.Import(target.RepoID, exported)
	if err != nil {
		return err
	}
	notifyLearningChanged(cwd)
	if *jsonOutput {
		return printJSON(profile)
	}
	fmt.Printf("enabled: %t\nprofile_version: %d\nlocal_samples: %d\n", profile.Enabled, profile.Version, profile.Local.Samples)
	return nil
}

func learningHook(action string, arguments []string) error {
	flags, cwdFlag, _ := commonFlags("learning " + action)
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return errors.New("invalid learning hook arguments")
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return err
	}
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: "learning_" + action, CWD: cwd}, 75*time.Millisecond)
	if err != nil || (reply.Status != "ready" && reply.Status != "ignored") {
		return nil
	}
	return nil
}

func learningScope(value string) (string, gitstate.State, *learning.Store, error) {
	cwd, err := resolveCWD(value)
	if err != nil {
		return "", gitstate.State{}, nil, err
	}
	context, cancel := gitstate.WithTimeout()
	state, err := gitstate.Snapshot(context, cwd)
	cancel()
	if err != nil {
		return "", gitstate.State{}, nil, err
	}
	if state.Root == "" || state.RepoID == "" {
		return "", gitstate.State{}, nil, errors.New("learning requires a Git worktree")
	}
	directory, err := runtimepath.DataDir()
	if err != nil {
		return "", gitstate.State{}, nil, err
	}
	store, err := learning.New(filepath.Join(directory, "learning"))
	if err != nil {
		return "", gitstate.State{}, nil, err
	}
	return cwd, state, store, nil
}

func readLearningExport(path string) (learning.Export, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return learning.Export{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 16*1024 {
		return learning.Export{}, errors.New("learning import is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return learning.Export{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16*1024))
	decoder.DisallowUnknownFields()
	var exported learning.Export
	if err := decoder.Decode(&exported); err != nil {
		return learning.Export{}, fmt.Errorf("decode learning import: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return learning.Export{}, errors.New("decode learning import: trailing content")
	}
	return exported, nil
}

func notifyLearningChanged(cwd string) {
	_, _ = call(ipc.Request{Version: ipc.Version, Operation: "learning_changed", CWD: cwd}, 100*time.Millisecond)
}

func activityCommand(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: zsh-git-inlay activity {inspect|clear} [--cwd <directory>] [--json]")
	}
	switch arguments[0] {
	case "inspect", "clear":
		return activityAdmin(arguments)
	case "emit":
		return activityEmit(arguments[1:])
	case "git-state":
		return activityGitState(arguments[1:])
	default:
		return errors.New("usage: zsh-git-inlay activity {inspect|clear} [--cwd <directory>] [--json]")
	}
}

func activityAdmin(arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags("activity " + arguments[0])
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return err
	}
	operation := "activity_" + arguments[0]
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: operation, CWD: cwd}, 100*time.Millisecond)
	if err != nil {
		return fmt.Errorf("daemon unavailable: %w", err)
	}
	if arguments[0] == "clear" {
		if reply.Status != "cleared" {
			return fmt.Errorf("activity clear %s: %s", reply.Status, reply.Error)
		}
		if *jsonOutput {
			return printJSON(map[string]string{"status": "cleared"})
		}
		fmt.Println("cleared")
		return nil
	}
	if reply.Status != "ready" {
		return fmt.Errorf("activity inspect %s: %s", reply.Status, reply.Error)
	}
	var inspection activity.Inspection
	if err := json.Unmarshal(reply.Payload, &inspection); err != nil {
		return fmt.Errorf("decode activity inspection: %w", err)
	}
	if *jsonOutput {
		return printJSON(inspection)
	}
	fmt.Printf("enabled: %t\nselected: %d\n", inspection.Enabled, len(inspection.Selected))
	for _, event := range inspection.Selected {
		fmt.Printf("%s %s %s %s\n", event.Timestamp.Format(time.RFC3339), event.Source, event.Kind, event.Sensitivity)
	}
	for reason, count := range inspection.Excluded {
		fmt.Printf("excluded %s: %d\n", reason, count)
	}
	return nil
}

func activityEmit(arguments []string) error {
	flags := flag.NewFlagSet("activity emit", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	cwdFlag := flags.String("cwd", "", "repository working directory")
	source := flags.String("source", "shell", "event source")
	kind := flags.String("kind", "", "event kind")
	sensitivity := flags.String("sensitivity", string(activity.Private), "event sensitivity")
	var values stringValues
	flags.Var(&values, "data", "bounded key=value event field")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *kind == "" {
		return errors.New("usage: zsh-git-inlay activity emit --cwd <directory> --source <source> --kind <kind> [--sensitivity <public|private|secret>] [--data key=value]")
	}
	enabled, err := activityEnabled()
	if err != nil || !enabled {
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
	if state.Root == "" || state.RepoID == "" || state.WorktreeID == "" {
		return nil
	}
	data := make(map[string]string, len(values))
	for _, value := range values {
		key, field, found := strings.Cut(value, "=")
		if !found || key == "" || field == "" || data[key] != "" {
			return errors.New("activity data must use unique nonempty key=value fields")
		}
		data[key] = field
	}
	event := activity.Event{Schema: activity.SchemaVersion, Repository: state.RepoID, Worktree: state.WorktreeID, Source: *source, Kind: *kind, Timestamp: time.Now().UTC(), Data: data, Sensitivity: activity.Sensitivity(*sensitivity)}
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: "event", CWD: cwd, Event: &event}, 100*time.Millisecond)
	if err != nil {
		return err
	}
	if reply.Status != "accepted" && reply.Status != "rejected" {
		return fmt.Errorf("activity event %s: %s", reply.Status, reply.Error)
	}
	return nil
}

func activityGitState(arguments []string) error {
	flags, cwdFlag, _ := commonFlags("activity git-state")
	commit := flags.Bool("commit", false, "record a completed commit only if HEAD changed")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		if err != nil {
			return err
		}
		return errors.New("usage: zsh-git-inlay activity git-state [--cwd <directory>]")
	}
	enabled, err := activityEnabled()
	if err != nil || !enabled {
		return err
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return err
	}
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: "activity_git_state", CWD: cwd, GitCommit: *commit}, 100*time.Millisecond)
	if err != nil {
		return err
	}
	if reply.Status != "ready" && reply.Status != "rejected" {
		return fmt.Errorf("activity Git state %s: %s", reply.Status, reply.Error)
	}
	return nil
}

func activityEnabled() (bool, error) {
	directory, err := runtimepath.DataDir()
	if err != nil {
		return false, err
	}
	permissions, err := activity.LoadPermissions(activity.PermissionsPath(directory))
	if err != nil {
		return false, err
	}
	return permissions.Activity, nil
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

type stringValues []string

func (values *stringValues) String() string { return strings.Join(*values, ",") }
func (values *stringValues) Set(value string) error {
	*values = append(*values, value)
	return nil
}

var _ = strings.Builder{}
