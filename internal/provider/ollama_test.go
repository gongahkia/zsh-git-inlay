package provider

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gongahkia/zsh-git-inlay/internal/config"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOllamaListsShowsAndGeneratesStructuredCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/tags":
			_, _ = writer.Write([]byte(`{"models":[{"name":"qwen2.5-coder:0.5b","model":"qwen2.5-coder:0.5b","digest":"abc","size":123,"details":{"quantization_level":"Q4_K_M"}}]}`))
		case "/api/show":
			_, _ = writer.Write([]byte(`{"details":{"quantization_level":"Q4_K_M"}}`))
		case "/api/generate":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode request: %v", err)
				return
			}
			if body["stream"] != false || body["format"] == nil || body["keep_alive"] != "5m0s" {
				t.Errorf("unbounded generation request: %#v", body)
			}
			_, _ = writer.Write([]byte(`{"model":"qwen2.5-coder:0.5b","done":true,"response":"{\"candidates\":[{\"type\":\"chore\",\"scope\":\"repo\",\"subject\":\"update staged files\",\"evidence_ids\":[\"change:0\"]}]}"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	ollama, err := NewOllama(server.URL, "qwen2.5-coder:0.5b", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	models, err := ollama.Models(context.Background())
	if err != nil || len(models) != 1 || models[0].Digest != "abc" {
		t.Fatalf("models=%#v err=%v", models, err)
	}
	shown, err := ollama.Show(context.Background())
	if err != nil || shown.Details.QuantizationLevel != "Q4_K_M" {
		t.Fatalf("show=%#v err=%v", shown, err)
	}
	response, err := ollama.Generate(context.Background(), Request{CWD: providerRepository(t)})
	if err != nil {
		t.Fatal(err)
	}
	values, err := response.ToCandidates()
	if err != nil || len(values) != 1 || values[0].Message != "chore(repo): update staged files" || response.Metadata.Quantization != "Q4_K_M" {
		t.Fatalf("response=%#v values=%#v err=%v", response, values, err)
	}
}

func TestOllamaRejectsRemoteAndMalformedOutput(t *testing.T) {
	if _, err := NewOllama("https://example.invalid", "model", time.Second); err == nil {
		t.Fatal("remote Ollama endpoint accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/generate" {
			_, _ = writer.Write([]byte(`{"done":true,"response":"{\"candidates\":[{\"type\":\"FIX\",\"subject\":\"bad\",\"evidence_ids\":[\"change:0\"]}]}"}`))
			return
		}
		_, _ = writer.Write([]byte(`{"details":{}}`))
	}))
	defer server.Close()
	ollama, err := NewOllama(server.URL, "model", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ollama.Generate(context.Background(), Request{CWD: providerRepository(t)}); err == nil {
		t.Fatal("malformed structured output accepted")
	}
}

func TestOllamaRejectsTrailingStructuredOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/generate" {
			_, _ = writer.Write([]byte(`{"done":true,"response":"{\"candidates\":[{\"type\":\"chore\",\"scope\":\"repo\",\"subject\":\"update staged files\",\"evidence_ids\":[\"change:0\"]}]} {}"}`))
			return
		}
		_, _ = writer.Write([]byte(`{"details":{}}`))
	}))
	defer server.Close()
	ollama, err := NewOllama(server.URL, "model", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ollama.Generate(context.Background(), Request{CWD: providerRepository(t)}); err == nil {
		t.Fatal("trailing structured output accepted")
	}
}

func TestOllamaHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) { <-request.Context().Done() }))
	defer server.Close()
	ollama, err := NewOllama(server.URL, "model", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := ollama.Generate(ctx, Request{CWD: providerRepository(t)}); err == nil {
		t.Fatal("cancelled Ollama request succeeded")
	}
}

func TestStructuredCandidateAllowsSafeSentenceCapitalization(t *testing.T) {
	response := Response{Candidates: []Candidate{{Type: "docs", Scope: "repo", Subject: "Update staged documentation", EvidenceIDs: []string{"change:0"}}}}
	values, err := response.ToCandidates()
	if err != nil || len(values) != 1 || values[0].Message != "docs(repo): Update staged documentation" {
		t.Fatalf("sentence-capitalized candidate = %#v err=%v", values, err)
	}
}

func providerRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	providerGit(t, repository, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repository, "file.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	providerGit(t, repository, "add", "file.txt")
	return repository
}

func providerGit(t *testing.T, cwd string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}

func TestOllamaConfiguredBudgetsAndGenerationMetrics(t *testing.T) {
	var truncated bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			_, _ = w.Write([]byte(`{"details":{}}`))
			return
		}
		var body struct {
			Options   map[string]int `json:"options"`
			KeepAlive string         `json:"keep_alive"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Options["num_ctx"] != 8192 || body.Options["num_predict"] != 512 || body.KeepAlive != "0s" {
			t.Errorf("settings not applied: %#v", body)
		}
		reason := "stop"
		if truncated {
			reason = "length"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"done": true, "done_reason": reason, "load_duration": 120, "prompt_eval_count": 20, "eval_count": 30, "response": `{"candidates":[{"type":"chore","subject":"update files","evidence_ids":["change:0"]}]}`})
	}))
	defer server.Close()
	settings := config.Default()
	settings.ProviderModel = "fixture"
	settings.OllamaContextTokens = 8192
	settings.OllamaOutputTokens = 512
	settings.OllamaKeepAlive = 0
	backend, err := NewConfiguredOllama(server.URL, settings)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Context: "private staged input"}
	response, err := backend.Generate(context.Background(), request)
	if err != nil || response.Metadata.Generation == nil || response.Metadata.Generation.LoadDuration != 120 || response.Metadata.Generation.EvalCount != 30 {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	truncated = true
	_, err = backend.Generate(context.Background(), request)
	var failure *GenerationError
	if !errors.As(err, &failure) || failure.Kind != "output_truncated" || strings.Contains(err.Error(), "private staged input") {
		t.Fatalf("failure=%v", err)
	}
}

func TestOllamaColdAndWarmTimeouts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			_, _ = w.Write([]byte(`{"details":{}}`))
			return
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"done": true, "response": `{"candidates":[{"type":"chore","subject":"update files","evidence_ids":["change:0"]}]}`})
	}))
	defer server.Close()
	settings := config.Default()
	settings.ProviderModel = "fixture"
	settings.ProviderTimeout = 10 * time.Millisecond
	settings.OllamaColdTimeout = time.Second
	backend, err := NewConfiguredOllama(server.URL, settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Generate(context.Background(), Request{Context: "fixture"}); err != nil {
		t.Fatalf("cold request: %v", err)
	}
	if _, err := backend.Generate(context.Background(), Request{Context: "fixture"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("warm timeout: %v", err)
	}
}
