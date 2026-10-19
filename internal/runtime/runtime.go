// Package runtime selects private XDG locations and verifies their ownership.
package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

func Dir() (string, error) {
	if override := os.Getenv("ZSH_GIT_INLAY_RUNTIME_DIR"); override != "" {
		return ensurePrivate(override)
	}
	if base := os.Getenv("XDG_RUNTIME_DIR"); base != "" {
		if path, err := ensurePrivate(filepath.Join(base, "zsh-git-inlay")); err == nil {
			return path, nil
		}
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home for runtime fallback: %w", err)
		}
		base = filepath.Join(home, ".cache")
	}
	return ensurePrivate(filepath.Join(base, "zsh-git-inlay", "runtime"))
}

func CacheDir() (string, error) {
	path, err := CachePath()
	if err != nil {
		return "", err
	}
	return ensurePrivate(path)
}

// CachePath resolves the location without changing the filesystem.
func CachePath() (string, error) {
	if override := os.Getenv("ZSH_GIT_INLAY_CACHE_DIR"); override != "" {
		return filepath.Abs(override)
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Abs(filepath.Join(base, "zsh-git-inlay", "cache"))
}

// StateDir stores durable local administrative data such as evaluation reports.
func StateDir() (string, error) {
	path, err := StatePath()
	if err != nil {
		return "", err
	}
	return ensurePrivate(path)
}

// StatePath resolves the location without changing the filesystem.
func StatePath() (string, error) {
	if override := os.Getenv("ZSH_GIT_INLAY_STATE_DIR"); override != "" {
		return filepath.Abs(override)
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Abs(filepath.Join(base, "zsh-git-inlay"))
}

func EvaluationDir() (string, error) {
	state, err := StateDir()
	if err != nil {
		return "", err
	}
	return ensurePrivate(filepath.Join(state, "evaluations"))
}

// DataDir stores user-approved managed runtimes and models.
func DataDir() (string, error) {
	path, err := DataPath()
	if err != nil {
		return "", err
	}
	return ensurePrivate(path)
}

// DataPath resolves the location without creating directories during lookup.
func DataPath() (string, error) {
	if override := os.Getenv("ZSH_GIT_INLAY_DATA_DIR"); override != "" {
		return filepath.Abs(override)
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Abs(filepath.Join(base, "zsh-git-inlay"))
}

func SocketPath() (string, error) {
	directory, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "daemon.sock"), nil
}

// RuntimePath resolves the preferred runtime directory without creating it.
// Dir may select its cache fallback if a configured XDG runtime is unwritable.
func RuntimePath() (string, error) {
	if override := os.Getenv("ZSH_GIT_INLAY_RUNTIME_DIR"); override != "" {
		return filepath.Abs(override)
	}
	if base := os.Getenv("XDG_RUNTIME_DIR"); base != "" {
		candidate := filepath.Join(base, "zsh-git-inlay")
		if runtimePrimaryUsable(candidate) {
			return filepath.Abs(candidate)
		}
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		userDirectory, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(userDirectory, ".cache")
	}
	return filepath.Abs(filepath.Join(base, "zsh-git-inlay", "runtime"))
}

func runtimePrimaryUsable(path string) bool {
	info, err := os.Lstat(path)
	if err == nil {
		stat, owned := info.Sys().(*syscall.Stat_t)
		return info.IsDir() && owned && int(stat.Uid) == os.Getuid()
	}
	if !os.IsNotExist(err) {
		return false
	}
	parent := filepath.Dir(path)
	for {
		if _, err := os.Stat(parent); err == nil {
			// POSIX access(2): W_OK is 2 and X_OK is 1 on supported Unix hosts.
			return syscall.Access(parent, 2|1) == nil
		} else if !os.IsNotExist(err) {
			return false
		}
		next := filepath.Dir(parent)
		if next == parent {
			return false
		}
		parent = next
	}
}

func ensurePrivate(path string) (string, error) {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create private directory %s: %w", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("private path %s is not a directory", path)
	}
	if runtime.GOOS != "windows" {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != os.Getuid() {
			return "", fmt.Errorf("private path %s is not owned by this user", path)
		}
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return "", fmt.Errorf("set private directory permissions: %w", err)
	}
	return path, nil
}
