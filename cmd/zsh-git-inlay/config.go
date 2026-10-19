package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/edit"
	"github.com/gongahkia/zsh-git-inlay/internal/lifecycle"
	runtimepath "github.com/gongahkia/zsh-git-inlay/internal/runtime"
)

func editStore() (edit.Store, error) {
	dir, err := runtimepath.StateDir()
	return edit.Store{Dir: filepath.Join(dir, "history")}, err
}

func configurationCommand(arguments []string) error {
	usage := errors.New("usage: zsh-git-inlay config {show|validate|set <key> <value>|reset <key>|repair} [--json] [--repo] [--cwd <repository>]")
	if len(arguments) == 0 {
		return usage
	}
	operation := arguments[0]
	flags, cwdFlag, jsonOutput := commonFlags("config " + operation)
	repository := flags.Bool("repo", false, "edit repository message policy instead of user preferences")
	// Support flags after the key/value too; a binding beginning with '-' can
	// be passed after '--'. No shell evaluation or environment expansion occurs.
	flagArgs, positional := []string{}, []string{}
	for i := 1; i < len(arguments); i++ {
		arg := arguments[i]
		if arg == "--" {
			positional = append(positional, arguments[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "--") {
			flagArgs = append(flagArgs, arg)
			if arg == "--cwd" && i+1 < len(arguments) {
				i++
				flagArgs = append(flagArgs, arguments[i])
			}
		} else {
			positional = append(positional, arg)
		}
	}
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	if (operation == "set" && len(positional) != 2) || (operation == "reset" && len(positional) != 1) || (operation != "set" && operation != "reset" && len(positional) != 0) {
		return usage
	}
	path := config.Path()
	if *repository {
		cwd, err := resolveCWD(*cwdFlag)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--show-toplevel")
		root, err := command.Output()
		if err != nil {
			return fmt.Errorf("find repository for config --repo: %w", err)
		}
		path = filepath.Join(strings.TrimSpace(string(root)), ".zsh-git-inlay.toml")
	}
	snapshot, err := edit.Read(path)
	if err != nil {
		return err
	}
	reportError := func(err error) error {
		if *jsonOutput {
			var diagnostic *config.Diagnostic
			if errors.As(err, &diagnostic) {
				_ = printJSON(map[string]any{"valid": false, "diagnostic": diagnostic})
			} else {
				_ = printJSON(map[string]any{"valid": false, "error": err.Error()})
			}
		}
		return err
	}
	var updated []byte
	var repairs []config.Diagnostic
	switch operation {
	case "show":
		entries, err := config.Show(snapshot.Data, path, *repository)
		if err != nil {
			return reportError(err)
		}
		if *jsonOutput {
			return printJSON(map[string]any{"path": path, "exists": snapshot.Exists, "entries": entries})
		}
		fmt.Println("Configuration:", path)
		for _, entry := range entries {
			fmt.Printf("%s = %v (default: %v; %s; source: %s)\n", entry.Key, entry.Effective, entry.Default, entry.Type, entry.Source)
		}
		return nil
	case "validate":
		if err := config.Validate(snapshot.Data, path, *repository); err != nil {
			return reportError(err)
		}
		if *jsonOutput {
			return printJSON(map[string]any{"valid": true, "path": path, "exists": snapshot.Exists})
		}
		fmt.Printf("Valid configuration: %s\n", path)
		return nil
	case "set":
		updated, err = config.Edit(snapshot.Data, path, positional[0], positional[1], false, *repository)
	case "reset":
		updated, err = config.Edit(snapshot.Data, path, positional[0], "", true, *repository)
	case "repair":
		var repair config.Repair
		repair, err = config.RepairDocument(snapshot.Data, path, *repository)
		updated, repairs = repair.Content, repair.Diagnostics
	default:
		return usage
	}
	if err != nil {
		return reportError(err)
	}
	if bytes.Equal(snapshot.Data, updated) {
		if *jsonOutput {
			return printJSON(map[string]any{"changed": false, "path": path})
		}
		fmt.Println("Configuration already matches:", path)
		return nil
	}
	store, err := editStore()
	if err != nil {
		return err
	}
	journal, err := store.Apply("config "+operation, []edit.Change{{Before: snapshot, Data: updated}})
	if err != nil {
		return err
	}
	backup := filepath.Join(store.Dir, journal.ID+".json")
	if *jsonOutput {
		return printJSON(map[string]any{"changed": true, "path": path, "transaction": journal.ID, "backup": backup, "repairs": repairs})
	}
	fmt.Printf("Updated %s\nBackup: %s\nUndo: zsh-git-inlay undo %s\n", path, backup, journal.ID)
	for _, diagnostic := range repairs {
		fmt.Println("Repaired:", diagnostic.Error())
	}
	return nil
}

func undoCommand(arguments []string) error {
	if len(arguments) != 1 {
		return errors.New("usage: zsh-git-inlay undo {list|<transaction-id>}")
	}
	store, err := editStore()
	if err != nil {
		return err
	}
	if arguments[0] == "list" {
		journals, err := store.Inspect()
		if err != nil {
			return err
		}
		for _, journal := range journals {
			fmt.Printf("%s  %s  %s\n", journal.ID, journal.State, journal.Label)
		}
		return nil
	}
	journal, err := store.UndoWith(arguments[0], lifecycle.MergeUndo)
	if err != nil {
		return err
	}
	fmt.Printf("Undid %s (%s)\n", journal.ID, journal.Label)
	return nil
}
