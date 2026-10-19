package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"github.com/gongahkia/zsh-git-inlay/internal/setup"
)

func setupCommand(arguments []string) error {
	flags, _, jsonOutput := commonFlags("setup")
	check := flags.Bool("check", false, "check the installed shell integration")
	demo := flags.Bool("demo", false, "exercise a disposable deterministic staged repository")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: zsh-git-inlay setup [--check] [--demo] [--json]")
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	installation := setup.Discover(binary)
	_, configErr := config.Load()
	installation.ConfigValid = configErr == nil
	var checkErr error
	if *check || *demo {
		checkErr = setup.Check(context.Background(), installation)
		installation.CheckPassed = checkErr == nil
		if checkErr != nil {
			installation.Problem = checkErr.Error()
		}
	}
	if configErr != nil {
		installation.Problem = configErr.Error()
		checkErr = configErr
	}
	suggestion := ""
	if *demo && checkErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		suggestion, checkErr = setup.Demo(ctx, binary)
		if checkErr != nil {
			installation.Problem = checkErr.Error()
		}
	}
	if *jsonOutput {
		if err := printJSON(map[string]any{"installation": installation, "demo_suggestion": suggestion}); err != nil {
			return err
		}
		return checkErr
	}
	if installation.Autosuggestions == "" {
		fmt.Println("Install zsh-autosuggestions or set ZSH_AUTOSUGGESTIONS_SOURCE to its source file.")
	} else {
		fmt.Printf("Add these lines to .zshrc in this order:\npath=(%s $path)\nsource %s\n", setup.Quote(filepath.Dir(binary)), setup.Quote(installation.Autosuggestions))
	}
	if installation.Plugin != "" {
		fmt.Printf("source %s\n", setup.Quote(installation.Plugin))
	} else {
		fmt.Println("Install the plugin alongside the executable before adding its source line.")
	}
	if installation.CheckPassed {
		fmt.Println("Installed shell integration: passed")
	}
	if suggestion != "" {
		fmt.Printf("Disposable demo: %s\nAccept a suggestion with your normal forward-character/end-of-line binding; cycle with the configured key.\n", suggestion)
	}
	return checkErr
}
