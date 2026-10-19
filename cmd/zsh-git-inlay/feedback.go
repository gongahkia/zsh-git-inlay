package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/daemon"
	"github.com/gongahkia/zsh-git-inlay/internal/feedback"
	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	runtimepath "github.com/gongahkia/zsh-git-inlay/internal/runtime"
)

func feedbackCommand(arguments []string) error {
	usage := errors.New("usage: zsh-git-inlay feedback {useful|too-generic|incorrect|show|clear} [--candidate 1..3] [--cwd directory] [--json]")
	if len(arguments) == 0 {
		return usage
	}
	operation := arguments[0]
	if !feedback.ValidLabel(operation) && operation != "show" && operation != "clear" {
		return usage
	}
	flags, cwdFlag, jsonOutput := commonFlags("feedback")
	index := flags.Int("candidate", 1, "one-based candidate number from candidates/compose")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *index < 1 || *index > 3 {
		return usage
	}
	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		return err
	}
	ctx, cancel := gitstate.WithTimeout()
	defer cancel()
	state, err := gitstate.Snapshot(ctx, cwd)
	if err != nil {
		return err
	}
	if state.RepoID == "" {
		return errors.New("feedback requires a Git repository")
	}
	dir, err := runtimepath.DataPath()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "feedback.json")
	if operation == "show" {
		stored, err := feedback.Load(path)
		if err != nil {
			return err
		}
		records := []feedback.Record{}
		counts := map[string]int{"useful": 0, "too-generic": 0, "incorrect": 0}
		for _, record := range stored.Records {
			if record.Repository == state.RepoID {
				records = append(records, record)
				counts[record.Label]++
			}
		}
		if *jsonOutput {
			return printJSON(map[string]any{"repository_id": state.RepoID, "counts": counts, "records": records, "path": path})
		}
		fmt.Printf("Local feedback: useful=%d, too-generic=%d, incorrect=%d\n", counts["useful"], counts["too-generic"], counts["incorrect"])
		return nil
	}
	if operation == "clear" {
		if err := feedback.Clear(path, state.RepoID); err != nil {
			return err
		}
		if *jsonOutput {
			return printJSON(map[string]any{"cleared": true, "repository_id": state.RepoID})
		}
		fmt.Println("Cleared this repository's local feedback.")
		return nil
	}
	if state.Availability != gitstate.Ready {
		return errors.New("stage changes and prepare candidates before labelling a suggestion")
	}
	reply, err := call(ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID}, 200*time.Millisecond)
	if err != nil {
		return fmt.Errorf("lookup feedback candidate: %w", err)
	}
	var record daemon.Record
	if reply.Status != "ready" || json.Unmarshal(reply.Payload, &record) != nil || *index > len(record.Candidates) {
		return errors.New("the requested candidate is unavailable; prepare and inspect the current candidates first")
	}
	entry := feedback.Record{Repository: state.RepoID, Worktree: state.WorktreeID, Fingerprint: state.Fingerprint, MessageHash: feedback.Hash(record.Candidates[*index-1].Message), Label: operation}
	if err := feedback.Add(path, entry); err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(map[string]any{"recorded": true, "label": operation, "candidate_hash": entry.MessageHash, "fingerprint": state.Fingerprint})
	}
	fmt.Printf("Recorded local feedback: %s (candidate %d).\n", operation, *index)
	return nil
}
