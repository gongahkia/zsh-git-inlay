package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/gongahkia/zsh-git-inlay/internal/config"
)

type wizard struct {
	scanner *bufio.Scanner
	output  io.Writer
}

func newWizard(input io.Reader, output io.Writer) wizard {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1024), 64<<10)
	return wizard{scanner: scanner, output: output}
}

func (wizard wizard) ask(prompt, fallback string, choices []string) (string, error) {
	for {
		fmt.Fprintf(wizard.output, "%s [%s]: ", prompt, fallback)
		if !wizard.scanner.Scan() {
			if err := wizard.scanner.Err(); err != nil {
				return "", err
			}
			return "", errors.New("wizard input ended; no changes applied")
		}
		answer := strings.TrimSpace(wizard.scanner.Text())
		if answer == "" {
			answer = fallback
		}
		if len(choices) == 0 {
			return answer, nil
		}
		for _, choice := range choices {
			if answer == choice {
				return answer, nil
			}
		}
		fmt.Fprintln(wizard.output, "Choose:", strings.Join(choices, ", "))
	}
}

func (wizard wizard) preferences(settings config.Settings, values preferenceFlags) error {
	provider, err := wizard.ask("Provider: deterministic (local), ollama (local), openai (cloud)", settings.Provider, []string{"deterministic", "deterministic-baseline", "ollama", "openai"})
	if err != nil {
		return err
	}
	values["provider.name"] = provider
	if provider == "ollama" || provider == "openai" {
		model, err := wizard.ask("Model identifier (already installed for Ollama)", settings.ProviderModel, nil)
		if err != nil {
			return err
		}
		values["provider.model"] = model
		timeout, err := wizard.ask("Warm generation timeout, 1s..1m", settings.ProviderTimeout.String(), nil)
		if err != nil {
			return err
		}
		values["provider.timeout"] = timeout
		if provider == "openai" {
			values["provider.fallback"] = "none"
			fmt.Fprintln(wizard.output, "OpenAI still requires your explicit cloud context grant and API credential; this wizard does not grant data access.")
		}
	}
	questions := []struct {
		prompt, key, fallback string
		choices               []string
	}{
		{"Maximum candidate count, 1..3", "suggestions.candidate_count", strconv.Itoa(settings.CandidateCount), []string{"1", "2", "3"}},
		{"Maximum complete subject line, 16..160 bytes", "commit.line_length", strconv.Itoa(settings.MessageLineLength), nil},
		{"Subject capitalization", "commit.capitalization", settings.MessageCaps, []string{"lower", "sentence"}},
		{"Scopes: auto or none", "commit.scope_mode", settings.ScopeMode, []string{"auto", "none"}},
		{"Cycle key binding (Zsh notation, or none)", "zsh.cycle_keybinding", settings.CycleKeybinding, nil},
		{"Use local commit-style learning", "learning.enabled", strconv.FormatBool(settings.LearningEnabled), []string{"true", "false"}},
	}
	if provider == "ollama" {
		questions = append(questions,
			struct {
				prompt, key, fallback string
				choices               []string
			}{"Ollama context tokens, 1024..32768", "ollama.context_tokens", strconv.Itoa(settings.OllamaContextTokens), nil},
			struct {
				prompt, key, fallback string
				choices               []string
			}{"Ollama output tokens, 64..2048", "ollama.output_tokens", strconv.Itoa(settings.OllamaOutputTokens), nil},
			struct {
				prompt, key, fallback string
				choices               []string
			}{"Ollama cold-start timeout, 1s..2m", "ollama.cold_timeout", settings.OllamaColdTimeout.String(), nil},
			struct {
				prompt, key, fallback string
				choices               []string
			}{"Ollama keep-alive, 0s..30m", "ollama.keep_alive", settings.OllamaKeepAlive.String(), nil},
		)
	}
	for _, question := range questions {
		answer, err := wizard.ask(question.prompt, question.fallback, question.choices)
		if err != nil {
			return err
		}
		values[question.key] = answer
	}
	advanced, err := wizard.ask("Customize allowed types/scopes, path mappings, keymaps and body policy", "no", []string{"yes", "no"})
	if err != nil || advanced == "no" {
		return err
	}
	arrays := []struct {
		prompt, key string
		current     []string
	}{
		{"Allowed commit types (comma-separated; - allows supported types)", "commit.types", settings.MessageTypes},
		{"Allowed scopes (comma-separated; - uses inferred scopes)", "commit.scopes", settings.MessageScopes},
		{"Scope path mappings (comma-separated relative-prefix=scope, or -)", "commit.scope_paths", settings.MessageScopePaths},
		{"Cycle keymaps (comma-separated emacs, viins, vicmd)", "zsh.cycle_keymaps", settings.CycleKeymaps},
	}
	for _, question := range arrays {
		answer, err := wizard.ask(question.prompt, strings.Join(question.current, ","), nil)
		if err != nil {
			return err
		}
		items := []string{}
		if answer == "-" {
			answer = ""
		}
		for _, item := range strings.Split(answer, ",") {
			if strings.TrimSpace(item) != "" {
				items = append(items, strings.TrimSpace(item))
			}
		}
		encoded, _ := json.Marshal(items)
		values[question.key] = string(encoded)
	}
	body, err := wizard.ask("Commit body policy", settings.MessageBody, []string{"forbid", "optional", "required"})
	if err != nil {
		return err
	}
	values["commit.body"] = body
	return nil
}

func (wizard wizard) review(preferences, shellFile string, values preferenceFlags) error {
	fmt.Fprintln(wizard.output, "Preferences:", preferences)
	if shellFile != "" {
		fmt.Fprintln(wizard.output, "Managed shell block:", shellFile)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(wizard.output, "  %s = %s\n", key, values[key])
	}
	answer, err := wizard.ask("Apply these changes with a private backup and undo", "no", []string{"yes", "no"})
	if err != nil {
		return err
	}
	if answer != "yes" {
		return errors.New("wizard cancelled; no changes applied")
	}
	return nil
}
