// Package config reads the deliberately small, declarative prototype config.
package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var policyName = regexp.MustCompile(`^[a-z][a-z0-9._/-]{0,39}$`)

const (
	DefaultIdleTimeout    = 15 * time.Minute
	ProviderPromptVersion = "v1"
)

const (
	DefaultCacheMaxRecords = 512
	DefaultCacheMaxBytes   = 32 * 1024 * 1024
	DefaultCacheMaxAge     = 7 * 24 * time.Hour
)

type Settings struct {
	IdleTimeout       time.Duration
	MaxRepositories   int
	MaxGenerationJobs int
	CacheMaxRecords   int
	CacheMaxBytes     int64
	CacheMaxAge       time.Duration
	Provider          string
	ProviderModel     string
	ProviderTimeout   time.Duration
	ProviderFallback  string
	GroundingPolicy   string
	CycleKeybinding   string
	Verbose           bool
	Version           string
}

func Default() Settings {
	return Settings{
		IdleTimeout:       DefaultIdleTimeout,
		MaxRepositories:   32,
		MaxGenerationJobs: 2,
		CacheMaxRecords:   DefaultCacheMaxRecords,
		CacheMaxBytes:     DefaultCacheMaxBytes,
		CacheMaxAge:       DefaultCacheMaxAge,
		Provider:          "deterministic",
		ProviderTimeout:   8 * time.Second,
		ProviderFallback:  "deterministic",
		GroundingPolicy:   "conservative",
		CycleKeybinding:   "^Xg",
		Version:           "default",
	}
}

func configPath() string {
	if value := os.Getenv("ZSH_GIT_INLAY_CONFIG"); value != "" {
		return value
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "zsh-git-inlay", "config.toml")
}

// Load never executes configuration. Unknown keys are errors so a repository
// cannot silently gain a capability as the format evolves.
func Load() (Settings, error) {
	settings := Default()
	path := configPath()
	if path == "" {
		return settings, nil
	}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return settings, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read config: %w", err)
	}
	section, values, err := parse(string(content), map[string]bool{
		"daemon.idle_timeout":               true,
		"daemon.max_active_repositories":    true,
		"daemon.max_generation_concurrency": true,
		"cache.max_records":                 true,
		"cache.max_bytes":                   true,
		"cache.max_age":                     true,
		"provider.name":                     true,
		"provider.model":                    true,
		"provider.timeout":                  true,
		"provider.fallback":                 true,
		"grounding.ambiguity":               true,
		"zsh.cycle_keybinding":              true,
		"diagnostics.verbose":               true,
	})
	_ = section
	if err != nil {
		return Settings{}, fmt.Errorf("invalid config %s: %w", path, err)
	}
	if value, ok := values["daemon.idle_timeout"]; ok {
		if !quoted(value) {
			return Settings{}, fmt.Errorf("invalid config %s: daemon.idle_timeout must be a string duration", path)
		}
		settings.IdleTimeout, err = time.ParseDuration(unquote(value))
		if err != nil || settings.IdleTimeout <= 0 {
			return Settings{}, fmt.Errorf("invalid config %s: daemon.idle_timeout must be positive", path)
		}
	}
	if value, ok := values["daemon.max_active_repositories"]; ok {
		settings.MaxRepositories, err = strconv.Atoi(value)
		if err != nil || settings.MaxRepositories < 1 || settings.MaxRepositories > 256 {
			return Settings{}, fmt.Errorf("invalid config %s: daemon.max_active_repositories must be 1..256", path)
		}
	}
	if value, ok := values["daemon.max_generation_concurrency"]; ok {
		settings.MaxGenerationJobs, err = strconv.Atoi(value)
		if err != nil || settings.MaxGenerationJobs < 1 || settings.MaxGenerationJobs > 16 {
			return Settings{}, fmt.Errorf("invalid config %s: daemon.max_generation_concurrency must be 1..16", path)
		}
	}
	if value, ok := values["cache.max_records"]; ok {
		settings.CacheMaxRecords, err = strconv.Atoi(value)
		if err != nil || settings.CacheMaxRecords < 1 || settings.CacheMaxRecords > 4096 {
			return Settings{}, fmt.Errorf("invalid config %s: cache.max_records must be 1..4096", path)
		}
	}
	if value, ok := values["cache.max_bytes"]; ok {
		settings.CacheMaxBytes, err = strconv.ParseInt(value, 10, 64)
		if err != nil || settings.CacheMaxBytes < 64*1024 || settings.CacheMaxBytes > 1024*1024*1024 {
			return Settings{}, fmt.Errorf("invalid config %s: cache.max_bytes must be 65536..1073741824", path)
		}
	}
	if value, ok := values["cache.max_age"]; ok {
		if !quoted(value) {
			return Settings{}, fmt.Errorf("invalid config %s: cache.max_age must be a string duration", path)
		}
		settings.CacheMaxAge, err = time.ParseDuration(unquote(value))
		if err != nil || settings.CacheMaxAge <= 0 || settings.CacheMaxAge > 365*24*time.Hour {
			return Settings{}, fmt.Errorf("invalid config %s: cache.max_age must be positive and at most 8760h", path)
		}
	}
	if value, ok := values["provider.name"]; ok {
		if !quoted(value) {
			return Settings{}, fmt.Errorf("invalid config %s: provider.name must be a string", path)
		}
		settings.Provider = unquote(value)
		if settings.Provider != "deterministic" && settings.Provider != "ollama" {
			return Settings{}, fmt.Errorf("invalid config %s: provider.name must be deterministic or ollama", path)
		}
	}
	if value, ok := values["provider.model"]; ok {
		if !quoted(value) {
			return Settings{}, fmt.Errorf("invalid config %s: provider.model must be a string", path)
		}
		settings.ProviderModel = unquote(value)
		if !safeProviderModel(settings.ProviderModel) {
			return Settings{}, fmt.Errorf("invalid config %s: provider.model contains unsupported characters", path)
		}
	}
	if value, ok := values["provider.timeout"]; ok {
		if !quoted(value) {
			return Settings{}, fmt.Errorf("invalid config %s: provider.timeout must be a string duration", path)
		}
		settings.ProviderTimeout, err = time.ParseDuration(unquote(value))
		if err != nil || settings.ProviderTimeout < time.Second || settings.ProviderTimeout > time.Minute {
			return Settings{}, fmt.Errorf("invalid config %s: provider.timeout must be 1s..1m", path)
		}
	}
	if value, ok := values["provider.fallback"]; ok {
		if !quoted(value) {
			return Settings{}, fmt.Errorf("invalid config %s: provider.fallback must be a string", path)
		}
		settings.ProviderFallback = unquote(value)
		if settings.ProviderFallback != "deterministic" && settings.ProviderFallback != "none" {
			return Settings{}, fmt.Errorf("invalid config %s: provider.fallback must be deterministic or none", path)
		}
	}
	if settings.Provider == "ollama" && settings.ProviderModel == "" {
		return Settings{}, fmt.Errorf("invalid config %s: provider.model is required for ollama", path)
	}
	if value, ok := values["grounding.ambiguity"]; ok {
		if !quoted(value) {
			return Settings{}, fmt.Errorf("invalid config %s: grounding.ambiguity must be a string", path)
		}
		settings.GroundingPolicy = unquote(value)
		if settings.GroundingPolicy != "conservative" && settings.GroundingPolicy != "quiet" && settings.GroundingPolicy != "visible" && settings.GroundingPolicy != "hintable" {
			return Settings{}, fmt.Errorf("invalid config %s: grounding.ambiguity must be conservative, quiet, visible, or hintable", path)
		}
	}
	if value, ok := values["zsh.cycle_keybinding"]; ok {
		if !quoted(value) {
			return Settings{}, fmt.Errorf("invalid config %s: zsh.cycle_keybinding must be a string", path)
		}
		settings.CycleKeybinding = unquote(value)
		if settings.CycleKeybinding == "" {
			return Settings{}, fmt.Errorf("invalid config %s: zsh.cycle_keybinding may not be empty", path)
		}
	}
	if value, ok := values["diagnostics.verbose"]; ok {
		settings.Verbose, err = strconv.ParseBool(value)
		if err != nil {
			return Settings{}, fmt.Errorf("invalid config %s: diagnostics.verbose must be true or false", path)
		}
	}
	sum := sha256.Sum256(content)
	settings.Version = fmt.Sprintf("%x", sum[:])
	return settings, nil
}

type ScopeRule struct {
	Path  string `json:"path"`
	Scope string `json:"scope"`
}

// RepositoryPolicy is intentionally declarative: it cannot grant a privacy or
// provider capability because its parser only accepts message-shape fields.
type RepositoryPolicy struct {
	Source         string      `json:"source"`
	Version        string      `json:"version"`
	Convention     string      `json:"convention"`
	Types          []string    `json:"types"`
	Scopes         []string    `json:"scopes"`
	ScopePaths     []ScopeRule `json:"scope_paths"`
	LineLength     int         `json:"line_length"`
	Capitalization string      `json:"capitalization"`
	Body           string      `json:"body"`
}

func DefaultRepositoryPolicy() RepositoryPolicy {
	return RepositoryPolicy{Source: "built-in", Version: "none", Convention: "conventional", LineLength: 120, Capitalization: "lower", Body: "optional"}
}

// LoadRepositoryPolicy validates only message-policy settings. It is not a
// general-purpose TOML parser and intentionally has no execution surface.
func LoadRepositoryPolicy(root string) (RepositoryPolicy, error) {
	policy := DefaultRepositoryPolicy()
	path := filepath.Join(root, ".zsh-git-inlay.toml")
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return policy, nil
	}
	if err != nil {
		return RepositoryPolicy{}, fmt.Errorf("read repository config: %w", err)
	}
	_, values, err := parse(string(content), map[string]bool{
		"commit.convention":     true,
		"commit.types":          true,
		"commit.scopes":         true,
		"commit.scope_paths":    true,
		"commit.line_length":    true,
		"commit.capitalization": true,
		"commit.body":           true,
	})
	if err != nil {
		return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: %w", path, err)
	}
	if value, ok := values["commit.convention"]; ok {
		if !quoted(value) || unquote(value) != "conventional" {
			return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: commit.convention must be conventional", path)
		}
		policy.Convention = unquote(value)
	}
	if value, ok := values["commit.types"]; ok {
		items, itemErr := policyArray(value, "commit.types")
		if itemErr != nil {
			return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: %w", path, itemErr)
		}
		for _, item := range items {
			if !policyName.MatchString(item) {
				return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: commit.types contains an unsafe type", path)
			}
		}
		policy.Types = items
	}
	if value, ok := values["commit.scopes"]; ok {
		items, itemErr := policyArray(value, "commit.scopes")
		if itemErr != nil {
			return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: %w", path, itemErr)
		}
		for _, item := range items {
			if !policyName.MatchString(item) {
				return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: commit.scopes contains an unsafe scope", path)
			}
		}
		policy.Scopes = items
	}
	if value, ok := values["commit.scope_paths"]; ok {
		items, itemErr := policyArray(value, "commit.scope_paths")
		if itemErr != nil {
			return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: %w", path, itemErr)
		}
		rules := make([]ScopeRule, 0, len(items))
		for _, item := range items {
			prefix, scope, found := strings.Cut(item, "=")
			prefix = strings.TrimSuffix(strings.TrimSpace(filepath.ToSlash(prefix)), "/")
			scope = strings.TrimSpace(scope)
			if !found || prefix == "" || filepath.IsAbs(prefix) || strings.HasPrefix(prefix, "../") || strings.Contains(prefix, "/../") || strings.ContainsAny(prefix, "\\\x00") || !policyName.MatchString(scope) {
				return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: commit.scope_paths entries must be relative-prefix=scope", path)
			}
			rules = append(rules, ScopeRule{Path: prefix, Scope: scope})
		}
		for _, rule := range rules {
			if len(policy.Scopes) > 0 && !contains(policy.Scopes, rule.Scope) {
				return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: commit.scope_paths scope must appear in commit.scopes", path)
			}
		}
		sort.Slice(rules, func(left, right int) bool {
			if len(rules[left].Path) == len(rules[right].Path) {
				return rules[left].Path < rules[right].Path
			}
			return len(rules[left].Path) > len(rules[right].Path)
		})
		policy.ScopePaths = rules
	}
	if value, ok := values["commit.line_length"]; ok {
		length, lengthErr := strconv.Atoi(value)
		if lengthErr != nil || length < 16 || length > 160 {
			return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: commit.line_length must be 16..160", path)
		}
		policy.LineLength = length
	}
	if value, ok := values["commit.capitalization"]; ok {
		if !quoted(value) || (unquote(value) != "lower" && unquote(value) != "sentence") {
			return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: commit.capitalization must be lower or sentence", path)
		}
		policy.Capitalization = unquote(value)
	}
	if value, ok := values["commit.body"]; ok {
		if !quoted(value) || (unquote(value) != "forbid" && unquote(value) != "optional" && unquote(value) != "required") {
			return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: commit.body must be forbid, optional, or required", path)
		}
		policy.Body = unquote(value)
	}
	sum := sha256.Sum256(content)
	policy.Source, policy.Version = path, fmt.Sprintf("%x", sum[:])
	return policy, nil
}

func RepositoryVersion(root string) (string, error) {
	policy, err := LoadRepositoryPolicy(root)
	if err != nil {
		return "", err
	}
	return policy.Version, nil
}

func (policy RepositoryPolicy) AllowsType(value string) bool {
	return len(policy.Types) == 0 || contains(policy.Types, value)
}

func (policy RepositoryPolicy) InferredScopes(paths []string) []string {
	result := make([]string, 0, len(policy.ScopePaths))
	seen := map[string]bool{}
	for _, path := range paths {
		path = strings.TrimPrefix(filepath.ToSlash(path), "./")
		for _, rule := range policy.ScopePaths {
			if path == rule.Path || strings.HasPrefix(path, rule.Path+"/") {
				if !seen[rule.Scope] {
					seen[rule.Scope] = true
					result = append(result, rule.Scope)
				}
				break
			}
		}
	}
	sort.Strings(result)
	return result
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func parse(content string, allowed map[string]bool) (string, map[string]string, error) {
	section := ""
	values := make(map[string]string)
	for lineNo, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if section == "" || strings.ContainsAny(section, " \t[]") {
				return section, nil, fmt.Errorf("line %d: invalid section", lineNo+1)
			}
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || section == "" {
			return section, nil, fmt.Errorf("line %d: expected key = value inside a section", lineNo+1)
		}
		key := section + "." + strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if !allowed[key] {
			return section, nil, fmt.Errorf("line %d: unsupported key %q", lineNo+1, key)
		}
		if value == "" || strings.ContainsAny(value, "\x00\r") {
			return section, nil, fmt.Errorf("line %d: invalid value", lineNo+1)
		}
		if _, exists := values[key]; exists {
			return section, nil, fmt.Errorf("line %d: duplicate key %q", lineNo+1, key)
		}
		values[key] = value
	}
	return section, values, nil
}

func unquote(value string) string {
	if quoted(value) {
		return value[1 : len(value)-1]
	}
	return value
}

func quoted(value string) bool {
	return len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\''))
}

func safeProviderModel(value string) bool {
	if value == "" || len(value) > 200 || strings.Contains(value, "://") {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && !strings.ContainsRune("._:/@-", character) {
			return false
		}
	}
	return true
}

func stringArray(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '[' || value[len(value)-1] != ']' {
		return false
	}
	content := strings.TrimSpace(value[1 : len(value)-1])
	if content == "" {
		return true
	}
	for _, item := range strings.Split(content, ",") {
		if !quoted(strings.TrimSpace(item)) {
			return false
		}
	}
	return true
}

func policyArray(value, key string) ([]string, error) {
	if !stringArray(value) {
		return nil, fmt.Errorf("%s must be a string array", key)
	}
	content := strings.TrimSpace(value[1 : len(value)-1])
	if content == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	items := make([]string, 0, strings.Count(content, ",")+1)
	for _, raw := range strings.Split(content, ",") {
		item := unquote(strings.TrimSpace(raw))
		if item == "" || len(item) > 80 || seen[item] {
			return nil, fmt.Errorf("%s contains an empty, oversized, or duplicate item", key)
		}
		seen[item] = true
		items = append(items, item)
	}
	return items, nil
}
