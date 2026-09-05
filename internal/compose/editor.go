package compose

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Editor struct{ arguments []string }

// GitEditor resolves only user-controlled editor sources. A repository-local
// core.editor cannot choose a program that compose executes. As with Git,
// GIT_EDITOR wins, followed by user-global core.editor, VISUAL, EDITOR, and
// the conventional vi fallback. It accepts only a simple executable plus
// literal arguments and never evaluates a shell editor string.
func GitEditor(ctx context.Context) (Editor, error) {
	setting := os.Getenv("GIT_EDITOR")
	if setting == "" {
		command := exec.CommandContext(ctx, "git", "config", "--global", "--get", "core.editor")
		if output, err := command.Output(); err == nil {
			setting = strings.TrimSpace(string(output))
		}
	}
	if setting == "" {
		setting = os.Getenv("VISUAL")
	}
	if setting == "" {
		setting = os.Getenv("EDITOR")
	}
	if setting == "" {
		setting = "vi"
	}
	if len(setting) > 4096 {
		return Editor{}, fmt.Errorf("configured Git editor is unavailable")
	}
	arguments := strings.Fields(setting)
	if len(arguments) == 0 || len(arguments) > 16 {
		return Editor{}, fmt.Errorf("configured Git editor is unsupported")
	}
	for _, argument := range arguments {
		if argument == "" || len(argument) > 1024 || strings.ContainsAny(argument, "\x00\r\n;&|$<>`'\"\\") {
			return Editor{}, fmt.Errorf("configured Git editor requires unsupported shell syntax")
		}
	}
	path, lookErr := exec.LookPath(arguments[0])
	if lookErr != nil {
		return Editor{}, fmt.Errorf("configured Git editor is unavailable")
	}
	switch filepath.Base(path) {
	case "sh", "bash", "dash", "zsh", "fish", "ksh", "env":
		return Editor{}, fmt.Errorf("configured Git editor cannot be a shell")
	}
	arguments[0] = path
	return Editor{arguments: arguments}, nil
}

func (editor Editor) Run(ctx context.Context, path string) error {
	if len(editor.arguments) == 0 || path == "" {
		return fmt.Errorf("configured Git editor is unavailable")
	}
	command := exec.CommandContext(ctx, editor.arguments[0], append(append([]string(nil), editor.arguments[1:]...), path)...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command.Run()
}

func readMessage(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !validMessageFile(info) {
		return "", fmt.Errorf("edited compose file is unavailable or exceeds the limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !validMessageFile(opened) || !os.SameFile(info, opened) {
		return "", fmt.Errorf("edited compose file changed while opening")
	}
	content, err := io.ReadAll(io.LimitReader(file, MaxMessageSize+1))
	if err != nil || len(content) > MaxMessageSize {
		return "", fmt.Errorf("edited compose file exceeds the limit")
	}
	return string(content), nil
}

func validMessageFile(info os.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0o077 == 0 && info.Size() <= MaxMessageSize
}
