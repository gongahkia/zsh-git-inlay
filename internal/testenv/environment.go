// Package testenv isolates repository tests from the user's shell, preferences,
// Git configuration and durable product data. It is imported only by tests.
package testenv

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func Isolate() (func(), error) {
	// Benchmarks build a real helper. Keep the compiler's existing caches
	// while isolating application HOME/XDG state, avoiding redundant downloads.
	compilerPaths := map[string]string{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if output, err := exec.CommandContext(ctx, "go", "env", "-json", "GOCACHE", "GOMODCACHE").Output(); err == nil {
		_ = json.Unmarshal(output, &compilerPaths)
	}
	cancel()
	root, err := os.MkdirTemp("/tmp", "zgi-unit-")
	if err != nil {
		return nil, err
	}
	previous := map[string]*string{}
	set := func(key, value string) error {
		if old, ok := os.LookupEnv(key); ok {
			copy := old
			previous[key] = &copy
		} else {
			previous[key] = nil
		}
		return os.Setenv(key, value)
	}
	for key, value := range compilerPaths {
		if value != "" {
			if err := set(key, value); err != nil {
				os.RemoveAll(root)
				return nil, err
			}
		}
	}
	for key, relative := range map[string]string{"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache", "XDG_STATE_HOME": "state", "XDG_RUNTIME_DIR": "runtime"} {
		if err := os.MkdirAll(filepath.Join(root, relative), 0o700); err != nil {
			os.RemoveAll(root)
			return nil, err
		}
		if err := set(key, filepath.Join(root, relative)); err != nil {
			os.RemoveAll(root)
			return nil, err
		}
	}
	for _, key := range []string{"ZDOTDIR", "ZSH_GIT_INLAY_CONFIG", "ZSH_GIT_INLAY_DATA_DIR", "ZSH_GIT_INLAY_CACHE_DIR", "ZSH_GIT_INLAY_STATE_DIR", "ZSH_GIT_INLAY_RUNTIME_DIR", "ZSH_GIT_INLAY_DISABLED", "OPENAI_API_KEY"} {
		if err := set(key, ""); err != nil {
			os.RemoveAll(root)
			return nil, err
		}
	}
	if err := set("GIT_CONFIG_GLOBAL", os.DevNull); err != nil {
		os.RemoveAll(root)
		return nil, err
	}
	if err := set("GIT_CONFIG_NOSYSTEM", "1"); err != nil {
		os.RemoveAll(root)
		return nil, err
	}
	return func() {
		for key, value := range previous {
			if value == nil {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, *value)
			}
		}
		_ = os.RemoveAll(root)
	}, nil
}
