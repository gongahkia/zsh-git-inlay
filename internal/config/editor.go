package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"
)

type Entry struct {
	Key       string `json:"key"`
	Type      string `json:"type"`
	Default   any    `json:"default"`
	Effective any    `json:"effective"`
	Source    string `json:"source"`
	Line      int    `json:"line,omitempty"`
}

var repositoryDefaults = []Entry{
	{Key: "commit.convention", Type: "string", Default: "conventional"},
	{Key: "commit.types", Type: "string array", Default: []string{}},
	{Key: "commit.scopes", Type: "string array", Default: []string{}},
	{Key: "commit.scope_paths", Type: "string array", Default: []string{}},
	{Key: "commit.line_length", Type: "integer", Default: 120},
	{Key: "commit.capitalization", Type: "string", Default: "lower"},
	{Key: "commit.body", Type: "string", Default: "optional"},
	{Key: "commit.scope_mode", Type: "string", Default: "auto"},
}

// Schema derives defaults from the same Settings used by the running daemon.
func Schema(repository bool) []Entry {
	if repository {
		return append([]Entry(nil), repositoryDefaults...)
	}
	return settingsEntries(Default())
}

func settingsEntries(settings Settings) []Entry {
	entries := []Entry{}
	value := reflect.ValueOf(settings)
	for i := 0; i < value.NumField(); i++ {
		key := value.Type().Field(i).Tag.Get("config")
		if key == "" {
			continue
		}
		field := value.Field(i)
		kind, effective := "string", field.Interface()
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			kind, effective = "duration", time.Duration(field.Int()).String()
		} else {
			switch field.Kind() {
			case reflect.Int, reflect.Int64:
				kind = "integer"
			case reflect.Bool:
				kind = "boolean"
			case reflect.Slice:
				kind = "string array"
				if field.IsNil() {
					effective = []string{}
				}
			}
		}
		entries = append(entries, Entry{Key: key, Type: kind, Default: effective, Effective: effective, Source: "built-in"})
	}
	return entries
}

func allowedKeys(repository bool) map[string]bool {
	keys := map[string]bool{}
	for _, entry := range Schema(repository) {
		keys[entry.Key] = true
	}
	return keys
}

func Validate(content []byte, path string, repository bool) error {
	if repository {
		settings, err := Load()
		if err != nil {
			return err
		}
		_, err = parseRepository(content, path, settings.RepositoryDefaults())
		return err
	}
	_, err := Parse(content, path)
	return err
}

func Show(content []byte, path string, repository bool) ([]Entry, error) {
	if err := Validate(content, path, repository); err != nil {
		return nil, err
	}
	doc, err := parseDocument(content, path, allowedKeys(repository))
	if err != nil {
		return nil, err
	}
	entries := Schema(repository)
	var effective []Entry
	globalEntries := map[string]Entry{}
	if repository {
		global, err := readContent(configPath())
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		entries, err := Show(global, configPath(), false)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			globalEntries[entry.Key] = entry
		}
	} else {
		settings, _ := Parse(content, path)
		effective = settingsEntries(settings)
	}
	for i := range entries {
		entry := &entries[i]
		entry.Source, entry.Effective = "built-in", entry.Default
		if repository {
			global := globalEntries[entry.Key]
			entry.Source, entry.Effective, entry.Line = global.Source, global.Effective, global.Line
		} else {
			entry.Effective = effective[i].Effective
		}
		if field, exists := doc.fields[entry.Key]; exists {
			entry.Source, entry.Line = path, field.line
			if repository {
				if err := json.Unmarshal([]byte(doc.values[entry.Key]), &entry.Effective); err != nil {
					return nil, err
				}
			}
		}
	}
	return entries, nil
}

// Edit changes one value while leaving unrelated bytes and trailing comments
// intact. The entire resulting document must validate before it can be saved.
// String/duration arguments accept plain text or an explicit TOML string.
func Edit(content []byte, path, key, value string, reset, repository bool) ([]byte, error) {
	var spec *Entry
	for _, entry := range Schema(repository) {
		if entry.Key == key {
			copy := entry
			spec = &copy
			break
		}
	}
	if spec == nil {
		return nil, fmt.Errorf("unknown configuration key %q", key)
	}
	doc, err := parseDocument(content, path, allowedKeys(repository))
	if err != nil {
		return nil, err
	}
	if !reset && (spec.Type == "string" || spec.Type == "duration") && !quoted(value) {
		encoded, _ := json.Marshal(value)
		value = string(encoded)
	}
	updated := append([]byte(nil), content...)
	if field, exists := doc.fields[key]; exists {
		start := field.valueStart
		if reset {
			start, value = field.start, ""
		}
		updated = append(append(append([]byte(nil), content[:start]...), value...), content[field.end:]...)
	} else if !reset {
		section, name, _ := strings.Cut(key, ".")
		insertion := name + " = " + value + "\n"
		at, exists := doc.sections[section]
		if !exists {
			// Root dotted keys work even if the existing document already has
			// another root key in this implicit table.
			at, insertion = 0, key+" = "+value+"\n"
		}
		if at > 0 && content[at-1] != '\n' {
			insertion = "\n" + insertion
		}
		updated = append(append(append([]byte(nil), content[:at]...), insertion...), content[at:]...)
	}
	if err := Validate(updated, path, repository); err != nil {
		return nil, err
	}
	return updated, nil
}

type Repair struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Content     []byte       `json:"-"`
}

// RepairDocument quarantines only invalid expressions as comments. Their exact
// original text remains in the document and in the caller's journal backup.
// It never enables a provider, activity collection, learning, or cloud consent.
func RepairDocument(content []byte, path string, repository bool) (Repair, error) {
	result := Repair{Content: append([]byte(nil), content...)}
	if len(content) > MaxConfigBytes {
		return result, fmt.Errorf("configuration exceeds 1 MiB; reduce it before repair")
	}
	for attempts := 0; attempts < 256; attempts++ {
		err := Validate(result.Content, path, repository)
		if err == nil {
			return result, nil
		}
		var diagnostic *Diagnostic
		if !errors.As(err, &diagnostic) {
			return result, err
		}
		if diagnostic.Path != path {
			return result, fmt.Errorf("repair prerequisite configuration first: %w", err)
		}
		result.Diagnostics = append(result.Diagnostics, *diagnostic)
		doc, _ := parseDocument(result.Content, path, nil)
		start, end := lineRange(result.Content, diagnostic.Line)
		if diagnostic.Message == "unsupported section" {
			// Leaving an unknown table's children active would reassign them
			// to the preceding table and could enable an unintended provider.
			end = doc.sections[diagnostic.Key]
		} else if field, exists := doc.fields[diagnostic.Key]; exists {
			start, _ = lineRange(result.Content, field.line)
			end = field.end
			for end < len(result.Content) && result.Content[end] != '\n' {
				end++
			}
			if end < len(result.Content) {
				end++
			}
		}
		if start == end {
			return result, fmt.Errorf("cannot isolate invalid expression at %s; backup and edit this document manually", diagnostic.Error())
		}
		var quarantine bytes.Buffer
		quarantine.WriteString("# zsh-git-inlay repair: quarantined invalid configuration\n")
		for _, line := range strings.Split(strings.TrimSuffix(string(result.Content[start:end]), "\n"), "\n") {
			quarantine.WriteString("# " + line + "\n")
		}
		result.Content = append(append(append([]byte(nil), result.Content[:start]...), quarantine.Bytes()...), result.Content[end:]...)
	}
	return result, errors.New("too many invalid expressions; no changes saved")
}

func lineRange(content []byte, line int) (int, int) {
	start := 0
	for i := 1; i < line && start < len(content); i++ {
		offset := bytes.IndexByte(content[start:], '\n')
		if offset < 0 {
			return len(content), len(content)
		}
		start += offset + 1
	}
	end := start + bytes.IndexByte(content[start:], '\n')
	if end < start {
		end = len(content)
	} else {
		end++
	}
	return start, end
}
