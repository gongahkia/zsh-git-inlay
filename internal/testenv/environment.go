// Package testenv isolates repository tests from the user's shell, preferences,
// Git configuration and durable product data. It is imported only by tests.
package testenv

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	remember := func(key string) {
		if _, recorded := previous[key]; recorded {
			return
		}
		if old, ok := os.LookupEnv(key); ok {
			copy := old
			previous[key] = &copy
		} else {
			previous[key] = nil
		}
	}
	set := func(key, value string) error {
		remember(key)
		return os.Setenv(key, value)
	}
	cleanup := func() {
		for key, value := range previous {
			if value == nil {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, *value)
			}
		}
		_ = os.RemoveAll(root)
	}
	ready := false
	defer func() {
		if !ready {
			cleanup()
		}
	}()
	// Git's inherited directory/index/object overrides can redirect fixture
	// writes outside their temporary repositories, even with a private HOME.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "ZSH_GIT_INLAY_") || key == "ZDOTDIR" || key == "OPENAI_API_KEY" {
			remember(key)
			if err := os.Unsetenv(key); err != nil {
				return nil, err
			}
		}
	}
	for key, value := range compilerPaths {
		if value != "" {
			if err := set(key, value); err != nil {
				return nil, err
			}
		}
	}
	for key, relative := range map[string]string{"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache", "XDG_STATE_HOME": "state", "XDG_RUNTIME_DIR": "runtime"} {
		if err := os.MkdirAll(filepath.Join(root, relative), 0o700); err != nil {
			return nil, err
		}
		if err := set(key, filepath.Join(root, relative)); err != nil {
			return nil, err
		}
	}
	if err := set("GIT_CONFIG_GLOBAL", os.DevNull); err != nil {
		return nil, err
	}
	if err := set("GIT_CONFIG_NOSYSTEM", "1"); err != nil {
		return nil, err
	}
	ready = true
	return cleanup, nil
}
