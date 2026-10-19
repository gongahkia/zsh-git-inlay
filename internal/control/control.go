// Package control implements local suggestion controls independent of generation
// fingerprints. Each lookup rereads them, including for already-cached results.
package control

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/edit"
	"github.com/gongahkia/zsh-git-inlay/internal/runtime"
)

const MaxRepositories = 128

type Rule struct {
	Disabled bool      `json:"disabled,omitempty"`
	Until    time.Time `json:"snoozed_until,omitempty"`
}

type State struct {
	Schema       int             `json:"schema"`
	Global       Rule            `json:"global"`
	Repositories map[string]Rule `json:"repositories"`
}

type Decision struct {
	Status string    `json:"status"`
	Scope  string    `json:"scope,omitempty"`
	Until  time.Time `json:"until,omitempty"`
}

func Path() (string, error) {
	dir, err := runtime.DataPath()
	return filepath.Join(dir, "controls.json"), err
}

func Load(path string) (State, error) {
	snapshot, err := edit.Read(path)
	if err != nil {
		return State{}, err
	}
	info, statErr := os.Lstat(path)
	if snapshot.Exists && (statErr != nil || snapshot.Mode.Perm()&0o077 != 0 || info.Mode()&os.ModeSymlink != 0) {
		return State{}, errors.New("controls must be a private regular file without a symlink")
	}
	return Decode(snapshot.Data)
}

func Decode(data []byte) (State, error) {
	state := State{Schema: 1, Repositories: map[string]Rule{}}
	if len(data) == 0 {
		return state, nil
	}
	if len(data) > 64*1024 {
		return state, errors.New("control file exceeds 64 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return state, fmt.Errorf("invalid controls: %w", err)
	}
	if state.Schema != 1 || len(state.Repositories) > MaxRepositories || decoder.Decode(new(any)) != io.EOF {
		return state, errors.New("unsupported or oversized control state")
	}
	if state.Repositories == nil {
		state.Repositories = map[string]Rule{}
	}
	for repository := range state.Repositories {
		if !ValidID(repository) {
			return state, errors.New("invalid repository in control state")
		}
	}
	return state, nil
}

func ValidID(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (state State) Check(repository string, now time.Time) Decision {
	rules := []struct {
		name string
		rule Rule
	}{{"global", state.Global}}
	if repository != "" {
		rules = append(rules, struct {
			name string
			rule Rule
		}{"repository", state.Repositories[repository]})
	}
	for _, item := range rules {
		if item.rule.Disabled {
			return Decision{Status: "disabled", Scope: item.name}
		}
		if now.Before(item.rule.Until) {
			return Decision{Status: "snoozed", Scope: item.name, Until: item.rule.Until}
		}
	}
	return Decision{Status: "enabled"}
}

func Check(path, repository string) (Decision, error) {
	state, err := Load(path)
	if err != nil {
		return Decision{Status: "controls_invalid"}, err
	}
	return state.Check(repository, time.Now()), nil
}

func Set(path, repository, operation string, duration time.Duration) (State, error) {
	var result State
	if repository != "" && !ValidID(repository) {
		return result, errors.New("invalid repository identity")
	}
	err := edit.UpdatePrivate(path, func(data []byte) ([]byte, error) {
		state, err := Decode(data)
		if err != nil {
			return nil, err
		}
		rule := state.Global
		if repository != "" {
			rule = state.Repositories[repository]
		}
		switch operation {
		case "enable":
			rule = Rule{}
		case "disable":
			rule = Rule{Disabled: true}
		case "snooze":
			if duration <= 0 || duration > 24*time.Hour {
				return nil, errors.New("snooze duration must be positive and at most 24h")
			}
			rule.Until = time.Now().UTC().Add(duration)
		default:
			return nil, errors.New("unsupported control operation")
		}
		if repository == "" {
			state.Global = rule
		} else if operation == "enable" {
			delete(state.Repositories, repository)
		} else {
			// Expired one-off snoozes should not consume repository slots.
			for id, item := range state.Repositories {
				if !item.Disabled && !time.Now().Before(item.Until) {
					delete(state.Repositories, id)
				}
			}
			if _, exists := state.Repositories[repository]; !exists && len(state.Repositories) >= MaxRepositories {
				return nil, errors.New("repository control limit reached; enable an unused repository first")
			}
			state.Repositories[repository] = rule
		}
		result = state
		return json.Marshal(state)
	})
	return result, err
}

// IsMissing is used by doctor to distinguish first-run defaults from corruption.
func IsMissing(path string) bool { _, err := os.Stat(path); return os.IsNotExist(err) }
