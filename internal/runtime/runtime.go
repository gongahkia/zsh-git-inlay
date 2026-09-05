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
	if override := os.Getenv("ZSH_GIT_INLAY_CACHE_DIR"); override != "" {
		return ensurePrivate(override)
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	return ensurePrivate(filepath.Join(base, "zsh-git-inlay", "cache"))
}

// StateDir stores durable local administrative data such as evaluation reports.
func StateDir() (string, error) {
	if override := os.Getenv("ZSH_GIT_INLAY_STATE_DIR"); override != "" {
		return ensurePrivate(override)
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return ensurePrivate(filepath.Join(base, "zsh-git-inlay"))
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
	if override := os.Getenv("ZSH_GIT_INLAY_DATA_DIR"); override != "" {
		return ensurePrivate(override)
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return ensurePrivate(filepath.Join(base, "zsh-git-inlay"))
}

func SocketPath() (string, error) {
	directory, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "daemon.sock"), nil
}

func ensurePrivate(path string) (string, error) {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create private directory %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return "", fmt.Errorf("set private directory permissions: %w", err)
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
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("private path %s is accessible by other users", path)
	}
	return path, nil
}
