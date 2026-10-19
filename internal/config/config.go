// Package config validates and edits declarative user and repository preferences.
package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var policyName = regexp.MustCompile(`^[a-z][a-z0-9._/-]{0,39}$`)

const (
	DefaultIdleTimeout    = 15 * time.Minute
	ProviderPromptVersion = "v2"
)

const (
	DefaultCacheMaxRecords = 512
	DefaultCacheMaxBytes   = 32 * 1024 * 1024
	DefaultCacheMaxAge     = 7 * 24 * time.Hour
)

type Settings struct {
	IdleTimeout         time.Duration `config:"daemon.idle_timeout"`
	MaxRepositories     int           `config:"daemon.max_active_repositories"`
	MaxGenerationJobs   int           `config:"daemon.max_generation_concurrency"`
	CacheMaxRecords     int           `config:"cache.max_records"`
	CacheMaxBytes       int64         `config:"cache.max_bytes"`
	CacheMaxAge         time.Duration `config:"cache.max_age"`
	Provider            string        `config:"provider.name"`
	ProviderModel       string        `config:"provider.model"`
	ProviderTimeout     time.Duration `config:"provider.timeout"`
	OllamaContextTokens int           `config:"ollama.context_tokens"`
	OllamaOutputTokens  int           `config:"ollama.output_tokens"`
	OllamaKeepAlive     time.Duration `config:"ollama.keep_alive"`
	OllamaColdTimeout   time.Duration `config:"ollama.cold_timeout"`
	ProviderFallback    string        `config:"provider.fallback"`
	GroundingPolicy     string        `config:"grounding.ambiguity"`
	ActivityRetention   time.Duration `config:"activity.retention"`
	ActivityMaxEvents   int           `config:"activity.max_events"`
	CycleKeybinding     string        `config:"zsh.cycle_keybinding"`
	CycleKeymaps        []string      `config:"zsh.cycle_keymaps"`
	Verbose             bool          `config:"diagnostics.verbose"`
	MessageConvention   string        `config:"commit.convention"`
	MessageTypes        []string      `config:"commit.types"`
	MessageScopes       []string      `config:"commit.scopes"`
	MessageScopePaths   []string      `config:"commit.scope_paths"`
	MessageLineLength   int           `config:"commit.line_length"`
	MessageCaps         string        `config:"commit.capitalization"`
	MessageBody         string        `config:"commit.body"`
	ScopeMode           string        `config:"commit.scope_mode"`
	CandidateCount      int           `config:"suggestions.candidate_count"`
	LearningEnabled     bool          `config:"learning.enabled"`
	Version             string
}

func Default() Settings {
	return Settings{
		IdleTimeout:         DefaultIdleTimeout,
		MaxRepositories:     32,
		MaxGenerationJobs:   2,
		CacheMaxRecords:     DefaultCacheMaxRecords,
		CacheMaxBytes:       DefaultCacheMaxBytes,
		CacheMaxAge:         DefaultCacheMaxAge,
		Provider:            "deterministic",
		ProviderTimeout:     8 * time.Second,
		OllamaContextTokens: 4096,
		OllamaOutputTokens:  256,
		OllamaKeepAlive:     5 * time.Minute,
		OllamaColdTimeout:   30 * time.Second,
		ProviderFallback:    "deterministic",
		GroundingPolicy:     "conservative",
		ActivityRetention:   30 * time.Minute,
		ActivityMaxEvents:   256,
		CycleKeybinding:     "^Xg",
		CycleKeymaps:        []string{"emacs", "viins"},
		MessageConvention:   "conventional",
		MessageTypes:        []string{},
		MessageScopes:       []string{},
		MessageScopePaths:   []string{},
		MessageLineLength:   120,
		MessageCaps:         "lower",
		MessageBody:         "optional",
		ScopeMode:           "auto",
		CandidateCount:      3,
		LearningEnabled:     true,
		Version:             "default",
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
	content, err := readContent(path)
	if os.IsNotExist(err) {
		return settings, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read config: %w", err)
	}
	return Parse(content, path)
}

// Path is the effective user configuration location, including XDG overrides.
func Path() string { return configPath() }

// Parse validates a document without reading or changing the environment.
func Parse(content []byte, path string) (settings Settings, resultErr error) {
	settings = Default()
	defer func() { resultErr = withLocation(resultErr, content, path) }()
	section, values, err := parse(string(content), allowedKeys(false))
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
		if settings.Provider != "deterministic" && settings.Provider != "deterministic-baseline" && settings.Provider != "ollama" && settings.Provider != "openai" {
			return Settings{}, fmt.Errorf("invalid config %s: provider.name must be deterministic, deterministic-baseline, ollama, or openai", path)
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
	for key, target := range map[string]*int{"ollama.context_tokens": &settings.OllamaContextTokens, "ollama.output_tokens": &settings.OllamaOutputTokens} {
		if value, ok := values[key]; ok {
			parsed, parseErr := strconv.Atoi(value)
			minimum, maximum := 1024, 32768
			if key == "ollama.output_tokens" {
				minimum, maximum = 64, 2048
			}
			if parseErr != nil || parsed < minimum || parsed > maximum {
				return Settings{}, fmt.Errorf("%s must be %d..%d", key, minimum, maximum)
			}
			*target = parsed
		}
	}
	for key, target := range map[string]*time.Duration{"ollama.keep_alive": &settings.OllamaKeepAlive, "ollama.cold_timeout": &settings.OllamaColdTimeout} {
		if value, ok := values[key]; ok {
			if !quoted(value) {
				return Settings{}, fmt.Errorf("%s must be a string duration", key)
			}
			parsed, parseErr := time.ParseDuration(unquote(value))
			minimum, maximum := time.Duration(0), 30*time.Minute
			if key == "ollama.cold_timeout" {
				minimum, maximum = time.Second, 2*time.Minute
			}
			if parseErr != nil || parsed < minimum || parsed > maximum {
				return Settings{}, fmt.Errorf("%s duration out of bounds", key)
			}
			*target = parsed
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
	if (settings.Provider == "ollama" || settings.Provider == "openai") && settings.ProviderModel == "" {
		return Settings{}, fmt.Errorf("invalid config %s: provider.model is required for %s", path, settings.Provider)
	}
	if settings.Provider == "openai" && settings.ProviderFallback != "none" {
		return Settings{}, fmt.Errorf("invalid config %s: provider.fallback must be none for openai", path)
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
	if value, ok := values["activity.retention"]; ok {
		if !quoted(value) {
			return Settings{}, fmt.Errorf("invalid config %s: activity.retention must be a string duration", path)
		}
		settings.ActivityRetention, err = time.ParseDuration(unquote(value))
		if err != nil || settings.ActivityRetention < time.Minute || settings.ActivityRetention > 24*time.Hour {
			return Settings{}, fmt.Errorf("invalid config %s: activity.retention must be 1m..24h", path)
		}
	}
	if value, ok := values["activity.max_events"]; ok {
		settings.ActivityMaxEvents, err = strconv.Atoi(value)
		if err != nil || settings.ActivityMaxEvents < 1 || settings.ActivityMaxEvents > 4096 {
			return Settings{}, fmt.Errorf("invalid config %s: activity.max_events must be 1..4096", path)
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
	if value, ok := values["zsh.cycle_keymaps"]; ok {
		settings.CycleKeymaps, err = policyArray(value, "zsh.cycle_keymaps")
		if err != nil || len(settings.CycleKeymaps) == 0 {
			return Settings{}, fmt.Errorf("zsh.cycle_keymaps must be a nonempty array of emacs, viins, or vicmd")
		}
		seen := map[string]bool{}
		for _, keymap := range settings.CycleKeymaps {
			if seen[keymap] || (keymap != "emacs" && keymap != "viins" && keymap != "vicmd") {
				return Settings{}, fmt.Errorf("invalid or repeated zsh.cycle_keymaps entry %q", keymap)
			}
			seen[keymap] = true
		}
	}
	if value, ok := values["diagnostics.verbose"]; ok {
		settings.Verbose, err = strconv.ParseBool(value)
		if err != nil {
			return Settings{}, fmt.Errorf("invalid config %s: diagnostics.verbose must be true or false", path)
		}
	}
	if value, ok := values["suggestions.candidate_count"]; ok {
		settings.CandidateCount, err = strconv.Atoi(value)
		if err != nil || settings.CandidateCount < 1 || settings.CandidateCount > 3 {
			return Settings{}, errors.New("suggestions.candidate_count must be 1..3")
		}
	}
	if value, ok := values["learning.enabled"]; ok {
		settings.LearningEnabled, err = strconv.ParseBool(value)
		if err != nil {
			return Settings{}, errors.New("learning.enabled must be true or false")
		}
	}
	messagePolicy, err := applyRepositoryValues(values, path, DefaultRepositoryPolicy())
	if err != nil {
		return Settings{}, err
	}
	settings.MessageConvention, settings.MessageTypes, settings.MessageScopes = messagePolicy.Convention, messagePolicy.Types, messagePolicy.Scopes
	if value, exists := values["commit.scope_paths"]; exists {
		settings.MessageScopePaths, _ = policyArray(value, "commit.scope_paths")
	}
	settings.MessageLineLength, settings.MessageCaps, settings.MessageBody, settings.ScopeMode = messagePolicy.LineLength, messagePolicy.Capitalization, messagePolicy.Body, messagePolicy.ScopeMode
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
	ScopeMode      string      `json:"scope_mode,omitempty"`
}

func DefaultRepositoryPolicy() RepositoryPolicy {
	return RepositoryPolicy{Source: "built-in", Version: "none", Convention: "conventional", LineLength: 120, Capitalization: "lower", Body: "optional", ScopeMode: "auto"}
}

// LoadRepositoryPolicy accepts only message-policy settings. Repository TOML
// cannot enable a provider or grant access to private data.
func LoadRepositoryPolicy(root string) (RepositoryPolicy, error) {
	settings, err := Load()
	if err != nil {
		return RepositoryPolicy{}, err
	}
	policy := settings.RepositoryDefaults()
	path := filepath.Join(root, ".zsh-git-inlay.toml")
	content, err := readContent(path)
	if os.IsNotExist(err) {
		return policy, nil
	}
	if err != nil {
		return RepositoryPolicy{}, fmt.Errorf("read repository config: %w", err)
	}
	return parseRepository(content, path, policy)
}

func readContent(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxConfigBytes {
		return nil, &Diagnostic{Path: path, Line: 1, Message: "expected a regular configuration file of at most 1 MiB"}
	}
	content, err := io.ReadAll(io.LimitReader(file, MaxConfigBytes+1))
	if err == nil && len(content) > MaxConfigBytes {
		err = &Diagnostic{Path: path, Line: 1, Message: "configuration exceeds 1 MiB"}
	}
	return content, err
}

func ParseRepository(content []byte, path string) (policy RepositoryPolicy, resultErr error) {
	return parseRepository(content, path, DefaultRepositoryPolicy())
}

func parseRepository(content []byte, path string, policy RepositoryPolicy) (result RepositoryPolicy, resultErr error) {
	defer func() { resultErr = withLocation(resultErr, content, path) }()
	_, values, err := parse(string(content), allowedKeys(true))
	if err != nil {
		return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: %w", path, err)
	}
	result, resultErr = applyRepositoryValues(values, path, policy)
	if resultErr != nil {
		return result, resultErr
	}
	sum := sha256.Sum256(append([]byte(policy.Version+"\n"), content...))
	result.Source, result.Version = path, fmt.Sprintf("%x", sum[:])
	return result, nil
}

func applyRepositoryValues(values map[string]string, path string, policy RepositoryPolicy) (RepositoryPolicy, error) {
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
	for _, rule := range policy.ScopePaths {
		if len(policy.Scopes) > 0 && !contains(policy.Scopes, rule.Scope) {
			return RepositoryPolicy{}, fmt.Errorf("invalid repository config %s: commit.scopes must include each inherited commit.scope_paths scope", path)
		}
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
	if value, ok := values["commit.scope_mode"]; ok {
		if !quoted(value) || (unquote(value) != "auto" && unquote(value) != "none") {
			return RepositoryPolicy{}, errors.New("commit.scope_mode must be auto or none")
		}
		policy.ScopeMode = unquote(value)
	}
	return policy, nil
}

// RepositoryDefaults layers user message preferences beneath repository policy.
func (settings Settings) RepositoryDefaults() RepositoryPolicy {
	policy := DefaultRepositoryPolicy()
	policy.Convention, policy.Types, policy.Scopes = settings.MessageConvention, settings.MessageTypes, settings.MessageScopes
	policy.LineLength, policy.Capitalization, policy.Body, policy.ScopeMode = settings.MessageLineLength, settings.MessageCaps, settings.MessageBody, settings.ScopeMode
	for _, mapping := range settings.MessageScopePaths {
		prefix, scope, _ := strings.Cut(mapping, "=")
		policy.ScopePaths = append(policy.ScopePaths, ScopeRule{Path: strings.TrimSuffix(strings.TrimSpace(prefix), "/"), Scope: strings.TrimSpace(scope)})
	}
	sort.Slice(policy.ScopePaths, func(i, j int) bool {
		if len(policy.ScopePaths[i].Path) == len(policy.ScopePaths[j].Path) {
			return policy.ScopePaths[i].Path < policy.ScopePaths[j].Path
		}
		return len(policy.ScopePaths[i].Path) > len(policy.ScopePaths[j].Path)
	})
	if settings.Version != "default" {
		policy.Source, policy.Version = configPath(), settings.Version
	}
	return policy
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
