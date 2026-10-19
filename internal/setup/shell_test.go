package setup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func shellEnvironment(t *testing.T, directory string) []string {
	t.Helper()
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh unavailable")
	}
	environment := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "HOME=") && !strings.HasPrefix(entry, "ZDOTDIR=") && !strings.HasPrefix(entry, "ZSH_GIT_INLAY_") {
			environment = append(environment, entry)
		}
	}
	return append(environment, "HOME="+directory)
}

func TestShellFileHonorsZdotdirFromZshenvAndNoisyStartup(t *testing.T) {
	dir, custom := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".zshenv"), []byte("print startup-noise\nZDOTDIR="+Quote(custom)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := ShellFile(context.Background(), "", shellEnvironment(t, dir))
	if err != nil || path != filepath.Join(custom, ".zshrc") {
		t.Fatalf("ZDOTDIR discovery: %q %v", path, err)
	}
	path, err = ShellFile(context.Background(), filepath.Join(dir, "explicit-rc"), []string{"PATH=/unavailable"})
	if err != nil || path != filepath.Join(dir, "explicit-rc") {
		t.Fatalf("explicit override sourced startup files: %q %v", path, err)
	}
}

func TestInteractiveProbesObserveActualPluginAndStrictBrokenConfigFallback(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	// A failing config helper exercises the plugin's safe binding defaults in
	// a user's strict shell without reading the maintainer's configuration.
	if err := os.WriteFile(filepath.Join(bin, "zsh-git-inlay"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	plugin, err := filepath.Abs("../../zsh-git-inlay.plugin.zsh")
	if err != nil {
		t.Fatal(err)
	}
	rc := "setopt errexit nounset\npath=(" + Quote(bin) + " $path)\n_zsh_autosuggest_fetch_suggestion() { :; }\nZSH_AUTOSUGGEST_USE_ASYNC=1\nZSH_AUTOSUGGEST_STRATEGY=(history)\nsource " + Quote(plugin) + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".zshrc"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, login := range []bool{false, true} {
		report, err := Probe(context.Background(), login, shellEnvironment(t, dir))
		if err != nil || !report.Loaded || !report.DependencyLoaded || !report.Async || report.PluginContract != PluginContract || len(report.Strategies) == 0 || report.Strategies[0] != "git-inlay" || report.CycleBinding != "^Xg" {
			t.Fatalf("actual shell activation: %+v %v", report, err)
		}
		if !strings.HasSuffix(report.PluginSource, "zsh-git-inlay.plugin.zsh") || !strings.Contains(report.Bindings["emacs"], "zsh-git-inlay-cycle") || report.Binary != filepath.Join(bin, "zsh-git-inlay") {
			t.Fatalf("probe concealed wrong source/binding/PATH: %+v", report)
		}
	}
}

func TestInteractiveProbeReportsMissingActivationAndBoundsStartupLifetime(t *testing.T) {
	dir := t.TempDir()
	environment := shellEnvironment(t, dir)
	report, err := Probe(context.Background(), false, environment)
	if err != nil || report.Loaded || report.DependencyLoaded {
		t.Fatalf("inactive shell: %+v %v", report, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".zshenv"), []byte("sleep 30\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := ShellFile(ctx, "", environment); err == nil {
		t.Fatal("unbounded startup reported success")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("shell probe left a startup child/process pipe blocking")
	}
}

func TestShellStartupOutputIsBounded(t *testing.T) {
	dir := t.TempDir()
	environment := shellEnvironment(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".zshenv"), []byte("repeat 10000 print -r -- startup-noise-startup-noise\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, _, err := runShell(context.Background(), environment, "-c", "print done")
	if err == nil || len(output) > 64<<10 {
		t.Fatalf("unbounded shell startup output: %d %v", len(output), err)
	}
}
