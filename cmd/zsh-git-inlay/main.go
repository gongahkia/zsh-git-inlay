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
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
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
	case "fingerprint":
		return fingerprint(arguments[1:])
	case "observe":
		return observe(arguments[1:])
	case "suggest":
		return suggest(arguments[1:])
	case "candidates":
		return candidates(arguments[1:])
	case "status":
		return status(arguments[1:])
	case "daemon":
		return daemonCommand(arguments[1:])
	default:
		return usage()
	}
}

func usage() error {
	return errors.New("usage: zsh-git-inlay {doctor|status|fingerprint|observe|suggest|candidates|daemon serve|daemon stop}")
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
	flags, cwdFlag, _ := commonFlags("suggest")
	buffer := flags.String("buffer", "", "current ZLE buffer")
	cycle := flags.Int("cycle", 0, "candidate index")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if len(*buffer) > 4096 {
		return nil
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return nil
	}
	context, cancel := gitstate.WithTimeout()
	defer cancel()
	state, err := gitstate.Snapshot(context, cwd)
	if err != nil || state.Availability != gitstate.Ready {
		return nil
	}
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID}, 20*time.Millisecond)
	if err != nil {
		_ = ensureDaemon(cwd)
		return nil
	}
	if reply.Status != "ready" {
		return nil
	}
	var record daemon.Record
	if json.Unmarshal(reply.Payload, &record) != nil {
		return nil
	}
	suggestion, ok := command.Suggestion(*buffer, len(*buffer), record.Candidates, *cycle)
	if ok {
		fmt.Print(suggestion)
	}
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
	socket, socketErr := runtimepath.SocketPath()
	state := "unavailable"
	if socketErr == nil {
		if _, err := call(ipc.Request{Version: ipc.Version, Operation: "status"}, 30*time.Millisecond); err == nil {
			state = "running"
		}
	}
	report := map[string]any{"zsh": zshErr == nil, "git": gitErr == nil, "zsh_autosuggestions_file": autosuggestions, "daemon": state, "runtime_socket": socket, "go": runtime.Version()}
	if *jsonOutput {
		return printJSON(report)
	}
	for _, key := range []string{"zsh", "git", "zsh_autosuggestions_file", "daemon", "runtime_socket", "go"} {
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
