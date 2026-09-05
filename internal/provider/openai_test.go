package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAIResponsesContractUsesAuthorizedStructuredNoStoreRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" || request.Method != http.MethodPost {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization header = %q", request.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "gpt-5" || body["store"] != false || body["stream"] != false || body["input"] != "[REDACTED]" || body["text"] == nil {
			t.Fatalf("unsafe or incomplete OpenAI request: %#v", body)
		}
		_, _ = writer.Write([]byte(`{"output":[{"content":[{"type":"output_text","text":"{\"candidates\":[{\"type\":\"fix\",\"scope\":\"api\",\"subject\":\"update staged parser\",\"evidence_ids\":[\"change:0\"]}]}"}]}]}`))
	}))
	defer server.Close()
	openai, err := newOpenAI(server.URL+"/v1", "gpt-5", time.Second, func() string { return "test-key" })
	if err != nil {
		t.Fatal(err)
	}
	response, err := openai.Generate(context.Background(), Request{Context: "[REDACTED]", Authorized: func(context.Context) bool { return true }, RetryAllowed: func(context.Context) bool { return true }})
	if err != nil || len(response.Candidates) != 1 || response.Metadata.Name != "openai" || response.Metadata.Settings["store"] != "false" {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	metadata, err := json.Marshal(response.Metadata)
	if err != nil || strings.Contains(string(metadata), "test-key") {
		t.Fatalf("provider metadata leaked credential: %q err=%v", metadata, err)
	}
}

func TestOpenAIDoesNotSendWithoutAuthorizationOrCredential(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	openai, err := newOpenAI(server.URL, "gpt-5", time.Second, func() string { return "test-key" })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openai.Generate(context.Background(), Request{Context: "safe", Authorized: func(context.Context) bool { return false }, RetryAllowed: func(context.Context) bool { return true }}); !errorsIs(err, errCloudUnauthorized) || calls.Load() != 0 {
		t.Fatalf("unauthorized send err=%v calls=%d", err, calls.Load())
	}
	openai.apiKey = func() string { return "" }
	if _, err := openai.Generate(context.Background(), Request{Context: "safe", Authorized: func(context.Context) bool { return true }, RetryAllowed: func(context.Context) bool { return true }}); err == nil || calls.Load() != 0 {
		t.Fatalf("missing credential send err=%v calls=%d", err, calls.Load())
	}
}

func TestOpenAIRetryIsBoundedAndStopsAfterSupersession(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write([]byte(`{"output":[{"content":[{"type":"output_text","text":"{\"candidates\":[{\"type\":\"fix\",\"subject\":\"update parser\",\"evidence_ids\":[\"change:0\"]}]}"}]}]}`))
	}))
	defer server.Close()
	openai, err := newOpenAI(server.URL, "gpt-5", time.Second, func() string { return "test-key" })
	if err != nil {
		t.Fatal(err)
	}
	openai.retryDelay = time.Millisecond
	var checks atomic.Int32
	if _, err := openai.Generate(context.Background(), Request{Context: "safe", Authorized: func(context.Context) bool { return true }, RetryAllowed: func(context.Context) bool { return checks.Add(1) == 1 }}); err == nil || calls.Load() != 1 {
		t.Fatalf("superseded retry err=%v calls=%d", err, calls.Load())
	}
	calls.Store(0)
	if _, err := openai.Generate(context.Background(), Request{Context: "safe", Authorized: func(context.Context) bool { return true }, RetryAllowed: func(context.Context) bool { return true }}); err != nil || calls.Load() != 2 {
		t.Fatalf("bounded retry err=%v calls=%d", err, calls.Load())
	}
}

func TestOpenAIHonorsCancellationWithoutLeakingRequestDetails(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		select {
		case <-request.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()
	openai, err := newOpenAI(server.URL, "gpt-5", time.Second, func() string { return "secret-key" })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := openai.Generate(ctx, Request{Context: "private prompt", Authorized: func(context.Context) bool { return true }, RetryAllowed: func(context.Context) bool { return true }})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("OpenAI request did not reach the cancellable mock server")
	}
	cancel()
	err = <-done
	if err == nil || strings.Contains(err.Error(), "secret-key") || strings.Contains(err.Error(), "private prompt") {
		t.Fatalf("cancellation err=%v", err)
	}
}

func errorsIs(err, target error) bool {
	return err == target || (err != nil && target != nil && strings.Contains(err.Error(), target.Error()))
}
