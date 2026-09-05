package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
			if body["stream"] != false || body["format"] == nil || body["keep_alive"] != "5m" {
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
