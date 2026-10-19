package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const PluginContract = "2"

type boundedBuffer struct {
	data  bytes.Buffer
	limit int
}

func (buffer *boundedBuffer) Bytes() []byte  { return buffer.data.Bytes() }
func (buffer *boundedBuffer) String() string { return buffer.data.String() }

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > buffer.limit-buffer.data.Len() {
		remaining := buffer.limit - buffer.data.Len()
		_, _ = buffer.data.Write(data[:remaining])
		return remaining, errors.New("shell probe output exceeded 64 KiB")
	}
	return buffer.data.Write(data)
}

// runShell bounds both output and lifetime, including subprocesses left behind
// by startup scripts. Inputs are positional arguments, never interpolated code.
func runShell(ctx context.Context, environment []string, arguments ...string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zsh", arguments...)
	cmd.Env = environment
	cmd.Stdin = strings.NewReader("")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 200 * time.Millisecond
	stdout, stderr := &boundedBuffer{limit: 64 << 10}, &boundedBuffer{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	return stdout.Bytes(), strings.TrimSpace(stderr.String()), errors.Join(err, ctx.Err())
}

// ShellFile observes ZDOTDIR after .zshenv runs, as Zsh does before reading
// interactive startup files. An explicit path skips startup-script execution.
func ShellFile(ctx context.Context, override string, environment []string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	script := `builtin print -rn -- $'\0ZGI_SHELL_FILE\0'"${ZDOTDIR:-$HOME}/.zshrc"$'\0'`
	output, stderr, err := runShell(ctx, environment, "-c", script)
	if err != nil {
		return "", fmt.Errorf("discover Zsh startup location: %s: %w", stderr, err)
	}
	fields := bytes.Split(output, []byte{0})
	for index, field := range fields {
		if string(field) == "ZGI_SHELL_FILE" && index+1 < len(fields) {
			path := string(fields[index+1])
			if path == "" || strings.ContainsAny(path, "\r\n\x00") {
				break
			}
			return filepath.Abs(path)
		}
	}
	return "", errors.New("Zsh startup scripts prevented discovery; specify --shell-file explicitly")
}

type ShellReport struct {
	Login            bool              `json:"login"`
	Loaded           bool              `json:"loaded"`
	DependencyLoaded bool              `json:"dependency_loaded"`
	PluginContract   string            `json:"plugin_contract"`
	PluginSource     string            `json:"plugin_source"`
	DependencySource string            `json:"dependency_source"`
	Binary           string            `json:"binary"`
	Strategies       []string          `json:"strategies"`
	Async            bool              `json:"async"`
	CycleBinding     string            `json:"cycle_binding"`
	CycleKeymaps     []string          `json:"cycle_keymaps"`
	Bindings         map[string]string `json:"bindings"`
	PreviousBindings map[string]string `json:"previous_bindings"`
	Stderr           string            `json:"startup_diagnostics,omitempty"`
}

// Probe runs the user's actual interactive startup files. It does not prepend
// the expected executable/plugin to PATH, which would conceal a broken setup.
func Probe(ctx context.Context, login bool, environment []string) (ShellReport, error) {
	const script = `emulate -L zsh
zmodload zsh/parameter
builtin print -rn -- $'\0ZGI_PROBE\0'
builtin print -rn -- "${_ZSH_GIT_INLAY_LOADED:-0}"$'\0'"${+functions[_zsh_autosuggest_fetch_suggestion]}"$'\0'"${ZSH_GIT_INLAY_PLUGIN_CONTRACT:-unknown}"$'\0'
builtin print -rn -- "${functions_source[_zsh_autosuggest_strategy_git-inlay]:-}"$'\0'"${functions_source[_zsh_autosuggest_fetch_suggestion]:-}"$'\0'
builtin print -rn -- "${commands[zsh-git-inlay]:-}"$'\0'"${(j:,:)ZSH_AUTOSUGGEST_STRATEGY}"$'\0'"${+ZSH_AUTOSUGGEST_USE_ASYNC}"$'\0'"${ZSH_GIT_INLAY_CYCLE_KEYBINDING:-}"$'\0'"${(j:,:)ZSH_GIT_INLAY_CYCLE_KEYMAPS}"$'\0'
for zgi_map in emacs viins vicmd; do
  builtin print -rn -- "$zgi_map"$'\0'"$(bindkey -M "$zgi_map" "${ZSH_GIT_INLAY_CYCLE_KEYBINDING:-^Xg}" 2>/dev/null)"$'\0'"${_ZSH_GIT_INLAY_CYCLE_ORIGINAL_WIDGETS[$zgi_map]:-}"$'\0'
done
builtin print -rn -- $'ZGI_END\0'`
	mode := "-ic"
	if login {
		mode = "-lic"
	}
	output, stderr, err := runShell(ctx, environment, mode, script)
	result := ShellReport{Login: login, Stderr: stderr, Bindings: map[string]string{}, PreviousBindings: map[string]string{}}
	fields := bytes.Split(output, []byte{0})
	for index, field := range fields {
		if string(field) != "ZGI_PROBE" {
			continue
		}
		data := fields[index+1:]
		if len(data) < 20 || string(data[19]) != "ZGI_END" {
			break
		}
		result.Loaded, result.DependencyLoaded = string(data[0]) == "1", string(data[1]) == "1"
		result.PluginContract, result.PluginSource, result.DependencySource, result.Binary = string(data[2]), string(data[3]), string(data[4]), string(data[5])
		if len(data[6]) > 0 {
			result.Strategies = strings.Split(string(data[6]), ",")
		}
		result.Async, result.CycleBinding = string(data[7]) == "1", string(data[8])
		if len(data[9]) > 0 {
			result.CycleKeymaps = strings.Split(string(data[9]), ",")
		}
		for offset := 10; offset < 19; offset += 3 {
			result.Bindings[string(data[offset])] = string(data[offset+1])
			result.PreviousBindings[string(data[offset])] = string(data[offset+2])
		}
		return result, err
	}
	return result, fmt.Errorf("interactive shell did not complete its activation probe: %s: %w", stderr, errors.Join(err, io.ErrUnexpectedEOF))
}
