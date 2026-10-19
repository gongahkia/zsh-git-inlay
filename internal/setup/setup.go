// Package setup checks installation without modifying shell configuration.
package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Installation struct {
	Binary          string `json:"binary"`
	Plugin          string `json:"plugin"`
	Autosuggestions string `json:"autosuggestions"`
	Git             bool   `json:"git"`
	Zsh             bool   `json:"zsh"`
	ConfigValid     bool   `json:"config_valid"`
	CheckPassed     bool   `json:"check_passed"`
	Problem         string `json:"problem,omitempty"`
}

func readable(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	return err == nil && info.Mode().IsRegular()
}

func Discover(binary string) Installation {
	result := Installation{Binary: binary}
	_, err := exec.LookPath("git")
	result.Git = err == nil
	_, err = exec.LookPath("zsh")
	result.Zsh = err == nil
	for _, candidate := range []string{os.Getenv("ZSH_AUTOSUGGESTIONS_SOURCE"), "/opt/homebrew/share/zsh-autosuggestions/zsh-autosuggestions.zsh", "/usr/local/share/zsh-autosuggestions/zsh-autosuggestions.zsh", "/usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh"} {
		if candidate != "" && readable(candidate) {
			result.Autosuggestions = candidate
			break
		}
	}
	binaryPath, err := filepath.EvalSymlinks(binary)
	if err != nil {
		binaryPath = binary
	}
	root := filepath.Dir(filepath.Dir(binaryPath))
	for _, candidate := range []string{filepath.Join(root, "share", "zsh-git-inlay", "zsh-git-inlay.plugin.zsh"), filepath.Join(root, "zsh-git-inlay.plugin.zsh")} {
		if readable(candidate) {
			result.Plugin = candidate
			break
		}
	}
	return result
}

// Check sources only the discovered dependency and plugin in a clean shell.
// It verifies load order, strategy registration and executable discovery.
func Check(ctx context.Context, installation Installation) error {
	if !installation.Zsh || !installation.Git || installation.Plugin == "" || installation.Autosuggestions == "" {
		return fmt.Errorf("install Git, Zsh, zsh-autosuggestions and the plugin before checking setup")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	script := `setopt errexit nounset
path=("${1:h}" $path)
source "$2"
source "$3"
(( ${+functions[_zsh_autosuggest_strategy_git-inlay]} ))
[[ ${ZSH_AUTOSUGGEST_STRATEGY[1]} == git-inlay ]]
command "$1" version --json >/dev/null
zsh_git_inlay_unload`
	cmd := exec.CommandContext(ctx, "zsh", "-dfc", script, "zsh", installation.Binary, installation.Autosuggestions, installation.Plugin)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("installation check failed: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func Quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

// Demo exercises the installed daemon/socket/lookup path in a disposable repo
// and private deterministic configuration. It never commits or uses a model.
func Demo(ctx context.Context, binary string) (string, error) {
	root, err := os.MkdirTemp("/tmp", "zgi-demo-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(root)
	environment := os.Environ()
	for key, value := range map[string]string{"ZSH_GIT_INLAY_RUNTIME_DIR": filepath.Join(root, "run"), "ZSH_GIT_INLAY_CACHE_DIR": filepath.Join(root, "cache"), "ZSH_GIT_INLAY_DATA_DIR": filepath.Join(root, "data"), "ZSH_GIT_INLAY_CONFIG": filepath.Join(root, "config.toml")} {
		prefix := key + "="
		filtered := environment[:0]
		for _, entry := range environment {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		environment = append(filtered, prefix+value)
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("[provider]\nname = \"deterministic\"\n"), 0600); err != nil {
		return "", err
	}
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = environment
		return cmd.CombinedOutput()
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cleanup, binary, "daemon", "stop")
		cmd.Env = environment
		_ = cmd.Run()
	}()
	for _, args := range [][]string{{"init", "-q", "-b", "main", root}, {"-C", root, "add", "demo.txt"}} {
		if len(args) > 1 && args[0] == "-C" {
			if err := os.WriteFile(filepath.Join(root, "demo.txt"), []byte("example staged change\n"), 0600); err != nil {
				return "", err
			}
		}
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Env = environment
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("demo Git setup: %s: %w", output, err)
		}
	}
	if output, err := run("observe", "--cwd", root); err != nil {
		return "", fmt.Errorf("demo observation: %s: %w", output, err)
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		output, err := run("suggest", "--no-start", "--cwd", root, "--buffer", "git commit -m ", "--json")
		var result struct {
			Status     string `json:"status"`
			Suggestion string `json:"suggestion"`
		}
		if err == nil && json.Unmarshal(output, &result) == nil && result.Status == "ready" {
			return result.Suggestion, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("demo preparation: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
