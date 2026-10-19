package lifecycle

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/edit"
)

// VerifyBlock checks the entire owned block without interpreting shell code.
func VerifyBlock(content []byte, manifest Manifest) error {
	start, end, err := region(content)
	if err != nil {
		return err
	}
	if start < 0 {
		return errors.New("managed integration block is missing")
	}
	if !bytes.Equal(content[start:end], Block(manifest.Prefix, manifest.Autosuggestions)) {
		return errors.New("managed integration block differs from the recorded installation")
	}
	return nil
}

// Repair restores exact manifest-hashed artifacts from private journal backups,
// never from a potentially modified installed plugin. Unmarked shell text and
// privacy grants remain under the user's control.
func (manager Manager) Repair() (Result, error) {
	return manager.RepairWithConfiguration(nil)
}

func (manager Manager) RepairWithConfiguration(configuration *edit.Change) (Result, error) {
	var result Result
	if err := manager.History.Recover(); err != nil {
		return result, err
	}
	manifest, err := manager.Load()
	if err != nil {
		return result, err
	}
	result.Manifest = manifest
	changes := []edit.Change{}
	paths := make([]string, 0, len(manifest.Files))
	for path := range manifest.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var journals []edit.Journal
	for _, path := range paths {
		if err := rejectArtifactLink(path); err != nil {
			return result, err
		}
		current, err := edit.Read(path)
		if err != nil {
			return result, err
		}
		artifact := manifest.Files[path]
		if current.Target != artifact.Target {
			return result, fmt.Errorf("preserve retargeted artifact %s; restore its original installation path first", path)
		}
		if current.Exists && hash(current.Data) == artifact.Hash && current.Mode == artifact.Mode {
			continue
		}
		if journals == nil {
			journals, err = manager.History.List()
			if err != nil {
				return result, err
			}
		}
		var trusted []byte
		for _, journal := range journals {
			for _, change := range journal.Changes {
				if change.Before.Path != current.Path {
					continue
				}
				if hash(change.Data) == artifact.Hash && !change.Remove {
					trusted = change.Data
				}
				if change.Before.Exists && hash(change.Before.Data) == artifact.Hash {
					trusted = change.Before.Data
				}
			}
		}
		if trusted == nil {
			return result, fmt.Errorf("no verified backup for %s; reinstall from an explicitly selected trusted release/source", path)
		}
		changes = append(changes, edit.Change{Before: current, Data: trusted, Mode: artifact.Mode})
	}
	shell, err := edit.Read(manifest.ShellFile)
	if err != nil {
		return result, err
	}
	updated, err := ReplaceBlock(shell.Data, Block(manifest.Prefix, manifest.Autosuggestions), false)
	if err != nil {
		return result, err
	}
	changes = appendChanged(changes, edit.Change{Before: shell, Data: updated, ManagedBlock: true})
	if configuration != nil {
		if configuration.Remove {
			return result, errors.New("repair cannot remove user preferences")
		}
		if err := config.Validate(configuration.Data, configuration.Before.Path, false); err != nil {
			return result, err
		}
		changes = appendChanged(changes, *configuration)
	}
	if len(changes) == 0 {
		return result, nil
	}
	result.Journal, err = manager.History.Apply("doctor restore verified installation", changes)
	result.Changed = err == nil
	return result, err
}

// ArtifactProblems reports content, mode and target drift without modifying it.
func (manager Manager) ArtifactProblems(manifest Manifest) []error {
	var problems []error
	for path, artifact := range manifest.Files {
		if err := rejectArtifactLink(path); err != nil {
			problems = append(problems, err)
			continue
		}
		current, err := edit.Read(path)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if !current.Exists {
			problems = append(problems, fmt.Errorf("missing artifact %s", path))
			continue
		}
		if current.Target != artifact.Target || hash(current.Data) != artifact.Hash || current.Mode != artifact.Mode {
			problems = append(problems, fmt.Errorf("artifact differs from installation manifest: %s", path))
		}
	}
	sort.Slice(problems, func(i, j int) bool { return problems[i].Error() < problems[j].Error() })
	return problems
}
