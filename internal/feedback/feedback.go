// Package feedback stores explicit local usefulness labels independently of
// inferred commit acceptance. No candidate messages or source text are kept.
package feedback

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/control"
	"github.com/gongahkia/zsh-git-inlay/internal/edit"
)

const MaxRecords = 512

type Record struct {
	Repository  string    `json:"repository_id"`
	Worktree    string    `json:"worktree_id"`
	Fingerprint string    `json:"fingerprint"`
	MessageHash string    `json:"candidate_hash"`
	Label       string    `json:"label"`
	Created     time.Time `json:"created"`
}

type State struct {
	Schema  int      `json:"schema"`
	Records []Record `json:"records"`
}

func ValidLabel(label string) bool {
	return label == "useful" || label == "too-generic" || label == "incorrect"
}

func Hash(message string) string {
	sum := sha256.Sum256([]byte(message))
	return hex.EncodeToString(sum[:])
}

func decode(data []byte) (State, error) {
	state := State{Schema: 1, Records: []Record{}}
	if len(data) == 0 {
		return state, nil
	}
	if len(data) > 512*1024 {
		return state, errors.New("feedback file exceeds 512 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return state, fmt.Errorf("invalid feedback: %w", err)
	}
	if state.Schema != 1 || len(state.Records) > MaxRecords || decoder.Decode(new(any)) != io.EOF {
		return state, errors.New("unsupported or oversized feedback state")
	}
	for _, record := range state.Records {
		if err := validate(record); err != nil {
			return state, err
		}
	}
	return state, nil
}

func validate(record Record) error {
	if !control.ValidID(record.Repository) || !control.ValidID(record.Worktree) || !control.ValidID(record.Fingerprint) || !control.ValidID(record.MessageHash) || !ValidLabel(record.Label) || record.Created.IsZero() {
		return errors.New("invalid feedback record")
	}
	return nil
}

func Load(path string) (State, error) {
	snapshot, err := edit.Read(path)
	if err != nil {
		return State{}, err
	}
	info, statErr := os.Lstat(path)
	if snapshot.Exists && (statErr != nil || snapshot.Mode.Perm()&0o077 != 0 || info.Mode()&os.ModeSymlink != 0) {
		return State{}, errors.New("feedback must be a private regular file without a symlink")
	}
	return decode(snapshot.Data)
}

func Add(path string, record Record) error {
	if record.Created.IsZero() {
		record.Created = time.Now().UTC()
	}
	if err := validate(record); err != nil {
		return err
	}
	return edit.UpdatePrivate(path, func(data []byte) ([]byte, error) {
		state, err := decode(data)
		if err != nil {
			return nil, err
		}
		filtered := state.Records[:0]
		for _, existing := range state.Records {
			if existing.Repository == record.Repository && existing.Worktree == record.Worktree && existing.Fingerprint == record.Fingerprint && existing.MessageHash == record.MessageHash {
				continue
			}
			filtered = append(filtered, existing)
		}
		state.Records = append(filtered, record)
		if len(state.Records) > MaxRecords {
			state.Records = state.Records[len(state.Records)-MaxRecords:]
		}
		return json.Marshal(state)
	})
}

func Clear(path, repository string) error {
	if !control.ValidID(repository) {
		return errors.New("invalid feedback repository")
	}
	return edit.UpdatePrivate(path, func(data []byte) ([]byte, error) {
		state, err := decode(data)
		if err != nil {
			return nil, err
		}
		filtered := state.Records[:0]
		for _, record := range state.Records {
			if record.Repository != repository {
				filtered = append(filtered, record)
			}
		}
		state.Records = filtered
		return json.Marshal(state)
	})
}
