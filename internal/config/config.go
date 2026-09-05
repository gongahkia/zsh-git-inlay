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

const DefaultIdleTimeout = 15 * time.Minute

type Settings struct {
	IdleTimeout       time.Duration
	MaxRepositories   int
	MaxGenerationJobs int
	CycleKeybinding   string
	Verbose           bool
	Version           string
}

func Default() Settings {
	return Settings{
		IdleTimeout:       DefaultIdleTimeout,
		MaxRepositories:   32,
		MaxGenerationJobs: 2,
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
		"zsh.cycle_keybinding":              true,
		"diagnostics.verbose":               true,
	})
	_ = section
	if err != nil {
		return Settings{}, fmt.Errorf("invalid config %s: %w", path, err)
	}
	if value, ok := values["daemon.idle_timeout"]; ok {
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
	if value, ok := values["zsh.cycle_keybinding"]; ok {
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
	_, _, err = parse(string(content), map[string]bool{
		"commit.convention":  true,
		"commit.types":       true,
		"commit.scopes":      true,
		"commit.line_length": true,
	})
	if err != nil {
		return "", fmt.Errorf("invalid repository config %s: %w", path, err)
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
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		return value[1 : len(value)-1]
	}
	return value
}
