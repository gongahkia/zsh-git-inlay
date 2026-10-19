package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/control"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
)

func clientControl(repository string) (control.Decision, error) {
	if os.Getenv("ZSH_GIT_INLAY_DISABLED") == "1" {
		return control.Decision{Status: "disabled", Scope: "session"}, nil
	}
	path, err := control.Path()
	if err != nil {
		return control.Decision{Status: "controls_invalid"}, err
	}
	return control.Check(path, repository)
}

func controlCommand(operation string, arguments []string) error {
	flags, cwdFlag, jsonOutput := commonFlags(operation)
	repository := flags.Bool("repo", false, "apply to this repository, including its linked worktrees")
	duration := time.Duration(0)
	if operation == "snooze" {
		// Accept the natural 'snooze 30m --repo' command form.
		if len(arguments) == 0 {
			return errors.New("usage: zsh-git-inlay snooze <duration, at most 24h> [--repo] [--cwd directory] [--json]")
		}
		var err error
		duration, err = time.ParseDuration(arguments[0])
		if err != nil || duration <= 0 || duration > 24*time.Hour {
			return errors.New("snooze duration must be positive and at most 24h")
		}
		arguments = arguments[1:]
	}
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected control arguments; use --repo for repository scope")
	}
	id := ""
	if *repository {
		cwd, err := resolveCWD(*cwdFlag)
		if err != nil {
			return err
		}
		ctx, cancel := gitstate.WithTimeout()
		defer cancel()
		state, err := gitstate.Snapshot(ctx, cwd)
		if err != nil {
			return fmt.Errorf("repository identity is unavailable: %w", err)
		}
		if state.RepoID == "" {
			return fmt.Errorf("repository identity is unavailable: %s", state.Reason)
		}
		id = state.RepoID
	}
	path, err := control.Path()
	if err != nil {
		return err
	}
	var state control.State
	if operation == "status" {
		state, err = control.Load(path)
	} else {
		state, err = control.Set(path, id, operation, duration)
		if err == nil {
			// Persisted controls are independently enforced at lookup, even
			// if this best-effort notification misses an old/stopped daemon.
			_, _ = call(ipc.Request{Version: ipc.Version, Operation: "controls_changed"}, 100*time.Millisecond)
		}
	}
	if err != nil {
		return err
	}
	decision := state.Check(id, time.Now())
	if os.Getenv("ZSH_GIT_INLAY_DISABLED") == "1" {
		decision = control.Decision{Status: "disabled", Scope: "session"}
	}
	if *jsonOutput {
		return printJSON(map[string]any{"decision": decision, "repository_id": id, "path": path, "global": state.Global, "repository_rule": state.Repositories[id]})
	}
	scope := "global"
	if *repository {
		scope = "repository"
	}
	fmt.Printf("%s control: %s", scope, decision.Status)
	if decision.Scope != "" {
		fmt.Printf(" (effective scope: %s)", decision.Scope)
	}
	if !decision.Until.IsZero() {
		fmt.Printf(" until %s", decision.Until.Local().Format(time.RFC3339))
	}
	fmt.Println()
	return nil
}
