// Package cloud owns explicit, user-private grants for remote providers.
package cloud

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const SchemaVersion = 1

type ContextClass string

const (
	StagedDiff        ContextClass = "staged_diff"
	RepositoryContext ContextClass = "repository_context"
	History           ContextClass = "history"
	BranchIdentifiers ContextClass = "branch_identifiers"
	Activity          ContextClass = "activity"
	OutputExcerpts    ContextClass = "output_excerpts"
	maxGrantProviders              = 16
	maxGrantClasses                = 6
)

var allowedClasses = map[ContextClass]bool{
	StagedDiff: true, RepositoryContext: true, History: true,
	BranchIdentifiers: true, Activity: true, OutputExcerpts: true,
}

// Grant contains no repository identity, endpoint, credential, prompt, or
// source content. A later grant replaces all classes for that provider, so an
// expanded capability always requires a fresh explicit command.
type Grant struct {
	Provider string         `json:"provider"`
	Classes  []ContextClass `json:"classes"`
}

type permissions struct {
	Schema int     `json:"schema_version"`
	Grants []Grant `json:"grants"`
}

type Store struct{ path string }

func New(dataDir string) (*Store, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("cloud grant storage is unavailable")
	}
	return &Store{path: filepath.Join(dataDir, "cloud-grants.json")}, nil
}

func (store *Store) Path() string { return store.path }

func ValidProvider(provider string) bool { return provider == "openai" }

func ValidClass(class ContextClass) bool { return allowedClasses[class] }

func Classes() []ContextClass {
	return []ContextClass{StagedDiff, RepositoryContext, History, BranchIdentifiers, Activity, OutputExcerpts}
}

func NormalizeClasses(classes []ContextClass) ([]ContextClass, error) {
	if len(classes) == 0 || len(classes) > maxGrantClasses {
		return nil, fmt.Errorf("grant must include 1..%d context classes", maxGrantClasses)
	}
	seen := map[ContextClass]bool{}
	result := make([]ContextClass, 0, len(classes))
	for _, class := range classes {
		if !ValidClass(class) || seen[class] {
			return nil, fmt.Errorf("invalid context class %q", class)
		}
		seen[class] = true
		result = append(result, class)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result, nil
}

func SameClasses(left, right []ContextClass) bool {
	left, leftErr := NormalizeClasses(left)
	right, rightErr := NormalizeClasses(right)
	if leftErr != nil || rightErr != nil || len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (store *Store) Grant(provider string) (Grant, error) {
	if !ValidProvider(provider) {
		return Grant{}, fmt.Errorf("unsupported cloud provider %q", provider)
	}
	record, err := store.load()
	if err != nil {
		return Grant{}, err
	}
	for _, grant := range record.Grants {
		if grant.Provider == provider {
			return grant, nil
		}
	}
	return Grant{Provider: provider, Classes: []ContextClass{}}, nil
}

// Set replaces a provider's complete grant. It deliberately cannot merge a
// newly requested class into a prior consent record.
func (store *Store) Set(provider string, classes []ContextClass) (Grant, error) {
	if !ValidProvider(provider) {
		return Grant{}, fmt.Errorf("unsupported cloud provider %q", provider)
	}
	normalized, err := NormalizeClasses(classes)
	if err != nil {
		return Grant{}, err
	}
	record, err := store.load()
	if err != nil {
		return Grant{}, err
	}
	updated := Grant{Provider: provider, Classes: normalized}
	found := false
	for index := range record.Grants {
		if record.Grants[index].Provider == provider {
			record.Grants[index] = updated
			found = true
		}
	}
	if !found {
		record.Grants = append(record.Grants, updated)
	}
	if err := store.save(record); err != nil {
		return Grant{}, err
	}
	return updated, nil
}

func (store *Store) Revoke(provider string) error {
	if !ValidProvider(provider) {
		return fmt.Errorf("unsupported cloud provider %q", provider)
	}
	record, err := store.load()
	if err != nil {
		return err
	}
	kept := record.Grants[:0]
	for _, grant := range record.Grants {
		if grant.Provider != provider {
			kept = append(kept, grant)
		}
	}
	record.Grants = kept
	return store.save(record)
}

func (store *Store) Status() ([]Grant, error) {
	record, err := store.load()
	if err != nil {
		return nil, err
	}
	return append([]Grant{}, record.Grants...), nil
}

func (store *Store) load() (permissions, error) {
	info, err := os.Lstat(store.path)
	if os.IsNotExist(err) {
		return permissions{Schema: SchemaVersion, Grants: []Grant{}}, nil
	}
	if err != nil {
		return permissions{}, err
	}
	stat, owned := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() > 4096 || !owned || int(stat.Uid) != os.Getuid() {
		return permissions{}, fmt.Errorf("cloud grant record is not private")
	}
	file, err := os.Open(store.path)
	if err != nil {
		return permissions{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4096))
	decoder.DisallowUnknownFields()
	var record permissions
	if err := decoder.Decode(&record); err != nil || decoder.Decode(&struct{}{}) != io.EOF || !valid(record) {
		return permissions{}, fmt.Errorf("invalid cloud grant record")
	}
	return record, nil
}

func (store *Store) save(record permissions) error {
	if !valid(record) {
		return fmt.Errorf("invalid cloud grant record")
	}
	content, err := json.Marshal(record)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(store.path), ".cloud-grants-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(content)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, store.path)
}

func valid(record permissions) bool {
	if record.Schema != SchemaVersion || len(record.Grants) > maxGrantProviders {
		return false
	}
	seen := map[string]bool{}
	for index := range record.Grants {
		grant := &record.Grants[index]
		if !ValidProvider(grant.Provider) || seen[grant.Provider] {
			return false
		}
		classes, err := NormalizeClasses(grant.Classes)
		if err != nil || !SameClasses(classes, grant.Classes) {
			return false
		}
		grant.Classes = classes
		seen[grant.Provider] = true
	}
	sort.Slice(record.Grants, func(left, right int) bool {
		return strings.Compare(record.Grants[left].Provider, record.Grants[right].Provider) < 0
	})
	return true
}
