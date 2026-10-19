package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/control"
	"github.com/gongahkia/zsh-git-inlay/internal/daemon"
	"github.com/gongahkia/zsh-git-inlay/internal/edit"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	runtimepath "github.com/gongahkia/zsh-git-inlay/internal/runtime"
)

type doctorRepair struct {
	Kind        string `json:"kind"`
	Detail      string `json:"detail"`
	Transaction string `json:"transaction,omitempty"`
	Backup      string `json:"backup,omitempty"`
}

func recordRepair(kind, detail string, journal edit.Journal, store edit.Store) doctorRepair {
	result := doctorRepair{Kind: kind, Detail: detail, Transaction: journal.ID}
	if journal.ID != "" {
		result.Backup = filepath.Join(store.Dir, journal.ID+".json")
	}
	return result
}

func doctorFix(cwd string) ([]doctorRepair, error) {
	var repairs []doctorRepair
	var failures []error
	store, err := editStore()
	if err != nil {
		return repairs, err
	}
	headers, inspectErr := store.Inspect()
	if inspectErr != nil {
		return repairs, inspectErr
	}
	if err := store.Recover(); err != nil {
		return repairs, err
	}
	for _, header := range headers {
		if header.State == "prepared" || header.State == "undoing" {
			repairs = append(repairs, doctorRepair{Kind: "interrupted_transaction", Detail: "Recovered " + header.ID, Transaction: header.ID, Backup: filepath.Join(store.Dir, header.ID+".json")})
		}
	}
	fixConfig := func(path string, repository bool) error {
		snapshot, err := edit.Read(path)
		if err != nil {
			return err
		}
		if !snapshot.Exists {
			return nil
		}
		repair, err := config.RepairDocument(snapshot.Data, snapshot.Path, repository)
		if err != nil {
			return err
		}
		if bytes.Equal(snapshot.Data, repair.Content) {
			return nil
		}
		journal, err := store.Apply("doctor repair configuration", []edit.Change{{Before: snapshot, Data: repair.Content}})
		if err != nil {
			return err
		}
		repairs = append(repairs, recordRepair("configuration", fmt.Sprintf("Quarantined %d invalid expressions in %s", len(repair.Diagnostics), path), journal, store))
		return nil
	}
	if err := fixConfig(config.Path(), false); err != nil {
		failures = append(failures, err)
	}
	if root, err := doctorRepositoryRoot(cwd); err == nil {
		if err := fixConfig(filepath.Join(root, ".zsh-git-inlay.toml"), true); err != nil {
			failures = append(failures, err)
		}
	}
	manager, err := lifecycleManager()
	if err != nil {
		failures = append(failures, err)
	} else {
		if _, err := manager.Load(); err == nil {
			result, err := manager.Repair()
			if err != nil {
				failures = append(failures, err)
			} else if result.Changed {
				repairs = append(repairs, recordRepair("installation", "Restored verified managed files and integration block", result.Journal, manager.History))
			}
		} else if !os.IsNotExist(err) {
			failures = append(failures, err)
		}
	}
	if path, err := control.Path(); err == nil {
		if _, err := control.Load(path); err != nil {
			snapshot, readErr := edit.Read(path)
			info, statErr := os.Lstat(path)
			if readErr != nil || statErr != nil || info.Mode()&os.ModeSymlink != 0 {
				failures = append(failures, fmt.Errorf("preserve invalid control path %s: %w", path, err))
			} else {
				state, decodeErr := control.Decode(snapshot.Data)
				detail := "Restored private control-file permissions"
				if decodeErr != nil {
					state = control.State{Schema: 1, Global: control.Rule{Disabled: true}, Repositories: map[string]control.Rule{}}
					detail = "Quarantined corrupt controls; suggestions remain globally disabled until you explicitly enable them"
				}
				data, _ := json.Marshal(state)
				journal, writeErr := store.Apply("doctor repair private controls", []edit.Change{{Before: snapshot, Data: data, Mode: 0o600}})
				if writeErr != nil {
					failures = append(failures, writeErr)
				} else {
					repairs = append(repairs, recordRepair("controls", detail, journal, store))
				}
			}
		}
	}
	for _, resolve := range []func() (string, error){runtimepath.Dir, runtimepath.CacheDir} {
		if _, err := resolve(); err != nil {
			failures = append(failures, err)
		}
	}
	if detail, err := doctorFixDaemon(); err != nil {
		failures = append(failures, err)
	} else if detail != "" {
		repairs = append(repairs, doctorRepair{Kind: "daemon", Detail: detail})
	}
	return repairs, errors.Join(failures...)
}

func doctorFixDaemon() (string, error) {
	socket, err := runtimeSocketForDoctor()
	if err != nil {
		return "", err
	}
	state, inspectErr := daemon.InspectSocket(socket)
	if inspectErr != nil {
		return "", inspectErr
	}
	if state == "stale" {
		changed, err := daemon.RepairStaleSocket(socket)
		if changed {
			return "Removed a refused owned stale socket", err
		}
		return "", err
	}
	if state != "running" {
		return "", nil
	}
	reply, err := callSocket(socket, ipc.Request{Version: ipc.Version, Operation: "status"}, 200*time.Millisecond)
	if err != nil {
		return "", err
	}
	var status daemon.Status
	if err := json.Unmarshal(reply.Payload, &status); err != nil {
		return "", err
	}
	current := daemon.CurrentBuild(version, commit)
	if status.Build.SHA256 == "" || status.Build.SHA256 != current.SHA256 || status.Build.Protocol != ipc.Version {
		if err := stopForLifecycle(); err != nil {
			return "", err
		}
		return "Stopped the previous product daemon; the next prompt starts the current helper", nil
	}
	return "", nil
}

func doctorRepositoryRoot(cwd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--show-toplevel")
	command.WaitDelay = 200 * time.Millisecond
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(output))
	if root == "" || len(root) > 4096 {
		return "", errors.New("invalid repository root")
	}
	return root, nil
}
