package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/command"
	"github.com/gongahkia/zsh-git-inlay/internal/daemon"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
)

func repositoryDiagnostic(cwd, buffer string) daemon.Diagnostic {
	if decision, err := clientControl(""); err != nil || decision.Status != "enabled" {
		reason := "Suggestion control applies to " + decision.Scope
		if err != nil {
			reason = err.Error()
		}
		return daemon.Diagnostic{Status: decision.Status, Reason: reason, Action: daemon.DiagnosticAction(decision.Status)}
	}
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: "diagnose", CWD: cwd}, 2*time.Second)
	value := daemon.Diagnostic{Status: "daemon_unavailable"}
	if err == nil {
		if json.Unmarshal(reply.Payload, &value) != nil {
			value.Status = "malformed"
		}
	} else {
		ctx, cancel := gitstate.WithTimeout()
		state, snapshotErr := gitstate.Snapshot(ctx, cwd)
		cancel()
		if snapshotErr == nil && state.RepoID != "" {
			if decision, controlErr := clientControl(state.RepoID); controlErr != nil || decision.Status != "enabled" {
				value.Status = decision.Status
			}
		}
	}
	if buffer != "" {
		if _, ok := command.Parse(buffer, len(buffer)); !ok {
			value.Status = "unsupported_command"
		} else if value.Status == "ready" {
			ctx, cancel := gitstate.WithTimeout()
			defer cancel()
			state, snapshotErr := gitstate.Snapshot(ctx, cwd)
			if snapshotErr == nil && state.Availability == gitstate.Ready {
				lookup, lookupErr := call(ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID}, 100*time.Millisecond)
				var record daemon.Record
				if lookupErr == nil && lookup.Status == "ready" && json.Unmarshal(lookup.Payload, &record) == nil {
					if _, ok := command.Suggestion(buffer, len(buffer), record.Candidates, 0); !ok {
						value.Status = "constrained_no_match"
					}
				} else {
					value.Status = "stale"
				}
			} else {
				value.Status = "stale"
			}
		}
	}
	value.Action = daemon.DiagnosticAction(value.Status)
	return value
}

func printDiagnostic(value daemon.Diagnostic, jsonOutput bool) error {
	if jsonOutput {
		return printJSON(value)
	}
	fmt.Printf("%s\n", value.Status)
	if value.Reason != "" {
		fmt.Println(value.Reason)
	}
	fmt.Printf("action: %s\n", value.Action)
	return nil
}
