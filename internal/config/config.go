// Package config reads the deliberately small, declarative prototype config.
package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

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

// RepositoryVersion validates only future message-policy settings. It is not a
// general-purpose TOML parser and intentionally has no execution surface.
func RepositoryVersion(root string) (string, error) {
	path := filepath.Join(root, ".zsh-git-inlay.toml")
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "none", nil
	}
	if err != nil {
		return "", fmt.Errorf("read repository config: %w", err)
	}
	_, values, err := parse(string(content), map[string]bool{
		"commit.convention":  true,
		"commit.types":       true,
		"commit.scopes":      true,
		"commit.line_length": true,
	})
	if err != nil {
		return "", fmt.Errorf("invalid repository config %s: %w", path, err)
	}
	for key, value := range values {
		switch key {
		case "commit.convention":
			if !quoted(value) {
				return "", fmt.Errorf("invalid repository config %s: commit.convention must be a string", path)
			}
		case "commit.types", "commit.scopes":
			if !stringArray(value) {
				return "", fmt.Errorf("invalid repository config %s: %s must be a string array", path, key)
			}
		case "commit.line_length":
			length, lengthErr := strconv.Atoi(value)
			if lengthErr != nil || length < 1 || length > 200 {
				return "", fmt.Errorf("invalid repository config %s: commit.line_length must be 1..200", path)
			}
		}
	}
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum[:]), nil
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
