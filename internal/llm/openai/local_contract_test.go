package openai_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/openai"
)

// TestLocalModels_SingleChunkToolCall tests that Ollama-style single-chunk tool calls
// (where arguments are emitted completely in one chunk rather than streamed deltas) are handled properly.
func TestLocalModels_SingleChunkToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Single-chunk tool call payload typical of Ollama
		chunk := `data: {"id":"chatcmpl-local","object":"chat.completion.chunk","created":1700000000,"model":"llama3.3","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_ollama_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}]},"finish_reason":null}]}` + "\n\n"
		_, _ = w.Write([]byte(chunk))
		flusher.Flush()

		// Finish reason
		finishChunk := `data: {"id":"chatcmpl-local","object":"chat.completion.chunk","created":1700000000,"model":"llama3.3","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"
		_, _ = w.Write([]byte(finishChunk))
		flusher.Flush()

		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	provider, err := openai.New(openai.Config{
		BaseURL: server.URL + "/v1",
		Model:   "llama3.3",
		APIKey:  "ollama",
	})
	if err != nil {
		t.Fatalf("openai.New failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := provider.Stream(ctx, llm.Request{
		Messages: []llm.Message{{Role: "user", Text: "read main.go"}},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var toolCalls []*llm.ToolCall
	for ev := range stream {
		if ev.Type == llm.StreamToolCall && ev.ToolCall != nil {
			toolCalls = append(toolCalls, ev.ToolCall)
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.Name != "read_file" {
		t.Errorf("expected tool name 'read_file', got %q", tc.Name)
	}
	expectedArgs := `{"path":"main.go"}`
	if string(tc.Input) != expectedArgs {
		t.Errorf("expected input %q, got %q", expectedArgs, string(tc.Input))
	}
}

// TestLocalModels_OmittedUsage tests that streams omitting the usage chunk
// (typical of LM Studio or older Ollama versions) complete gracefully without error.
func TestLocalModels_OmittedUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)

		chunk := `data: {"id":"chatcmpl-local","choices":[{"index":0,"delta":{"content":"Hello from local model!"},"finish_reason":null}]}` + "\n\n"
		_, _ = w.Write([]byte(chunk))
		flusher.Flush()

		// No usage chunk emitted before [DONE]
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	provider, err := openai.New(openai.Config{
		BaseURL: server.URL + "/v1",
		Model:   "qwen2.5-coder",
		APIKey:  "local",
	})
	if err != nil {
		t.Fatalf("openai.New failed: %v", err)
	}

	stream, err := provider.Stream(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Text: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var text string
	for ev := range stream {
		if ev.Type == llm.StreamText {
			text += ev.Text
		}
	}

	if text != "Hello from local model!" {
		t.Fatalf("expected 'Hello from local model!', got %q", text)
	}
}

// TestLocalModels_CostCalculation verifies that models on loopback or with zero price
// report zero cost ($0.00).
func TestLocalModels_CostCalculation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		usageChunk := `data: {"id":"chatcmpl-local","choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500}}` + "\n\n"
		_, _ = w.Write([]byte(usageChunk))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	provider, err := openai.New(openai.Config{
		BaseURL: server.URL + "/v1",
		Model:   "llama3.3",
		APIKey:  "ollama",
	})
	if err != nil {
		t.Fatalf("openai.New failed: %v", err)
	}

	stream, err := provider.Stream(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Text: "test"}},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var usage *llm.Usage
	for ev := range stream {
		if ev.Type == llm.StreamUsage {
			usage = ev.Usage
		}
	}

	if usage == nil {
		t.Fatal("expected usage event")
	}
	if usage.Cost != 0.0 {
		t.Fatalf("local model cost must be $0.00, got %f", usage.Cost)
	}
}

// TestLocalModels_ServerNotRunning verifies that a connection failure to a local endpoint
// is classified with friendly guidance.
func TestLocalModels_ServerNotRunning(t *testing.T) {
	// Point to closed/unused port on 127.0.0.1
	unusedURL := "http://127.0.0.1:54321/v1"

	provider, err := openai.New(openai.Config{
		BaseURL: unusedURL,
		Model:   "llama3.3",
		APIKey:  "ollama",
	})
	if err != nil {
		t.Fatalf("openai.New failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := provider.Stream(ctx, llm.Request{
		Messages: []llm.Message{{Role: "user", Text: "test"}},
	})
	if err != nil {
		return
	}

	var streamErr error
	for ev := range stream {
		if ev.Type == llm.StreamError {
			streamErr = ev.Err
		}
	}

	if streamErr == nil {
		t.Fatal("expected connection error for stopped server")
	}
	errStr := fmt.Sprintf("%v", streamErr)
	if !strings.Contains(errStr, "connection refused") && !strings.Contains(errStr, "local model server") {
		t.Fatalf("expected friendly connection-refused error, got: %s", errStr)
	}
}

// TestBaseURLNormalization verifies that URLs with or without /v1 both route to /v1/chat/completions.
// Official documentation citations:
// - Ollama OpenAI compatibility: https://github.com/ollama/ollama/blob/main/docs/openai.md ("http://localhost:11434/v1")
// - LM Studio API endpoints: https://lmstudio.ai/docs/api/endpoints ("http://localhost:1234/v1")
func TestBaseURLNormalization(t *testing.T) {
	var receivedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPaths = append(receivedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	// 1. Without /v1 (typical Ollama config: http://127.0.0.1:11434)
	normWithout := openai.NormalizeBaseURL(server.URL)
	if !strings.HasSuffix(normWithout, "/v1") {
		t.Fatalf("expected /v1 suffix, got: %s", normWithout)
	}

	p1, err := openai.New(openai.Config{
		BaseURL: server.URL, // no /v1
		Model:   "llama3.3",
		APIKey:  "ollama",
	})
	if err != nil {
		t.Fatalf("openai.New without /v1 failed: %v", err)
	}

	stream1, err := p1.Stream(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Text: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream 1 failed: %v", err)
	}
	for range stream1 {
	}

	// 2. With /v1 (typical LM Studio config: http://127.0.0.1:1234/v1)
	normWith := openai.NormalizeBaseURL(server.URL + "/v1")
	if strings.HasSuffix(normWith, "/v1/v1") {
		t.Fatalf("double /v1 detected: %s", normWith)
	}

	p2, err := openai.New(openai.Config{
		BaseURL: server.URL + "/v1", // with /v1
		Model:   "qwen2.5-coder",
		APIKey:  "lmstudio",
	})
	if err != nil {
		t.Fatalf("openai.New with /v1 failed: %v", err)
	}

	stream2, err := p2.Stream(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Text: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream 2 failed: %v", err)
	}
	for range stream2 {
	}

	// Assert both requests hit /v1/chat/completions
	if len(receivedPaths) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(receivedPaths))
	}
	for i, path := range receivedPaths {
		if path != "/v1/chat/completions" {
			t.Errorf("request %d path was %q, expected /v1/chat/completions", i+1, path)
		}
	}
}

// TestLocalModels_ToolRejectionCapabilityError verifies that when a local model rejects
// the 'tools' parameter with HTTP 400, Forge returns a non-looping error suggesting 'plan' mode.
func TestLocalModels_ToolRejectionCapabilityError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"tools are not supported for this model: deepseek-r1:7b"}}`))
	}))
	defer server.Close()

	provider, err := openai.New(openai.Config{
		BaseURL: server.URL + "/v1",
		Model:   "deepseek-r1:7b",
		APIKey:  "ollama",
	})
	if err != nil {
		t.Fatalf("openai.New failed: %v", err)
	}

	stream, err := provider.Stream(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Text: "fix the test"}},
		Tools: []llm.ToolSpec{
			{Name: "bash", Description: "run bash command"},
		},
	})
	if err != nil {
		t.Fatalf("Stream unexpected initial error: %v", err)
	}

	var streamErr error
	for ev := range stream {
		if ev.Type == llm.StreamError {
			streamErr = ev.Err
		}
	}

	if streamErr == nil {
		t.Fatal("expected tool capability error from stream, got nil")
	}

	// Must fail fast without retrying (only 1 attempt)
	if attempts != 1 {
		t.Fatalf("expected exactly 1 attempt (non-looping), got %d attempts", attempts)
	}

	errMsg := streamErr.Error()
	if !strings.Contains(errMsg, "does not support tool calling") {
		t.Errorf("expected error to mention 'does not support tool calling', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "plan") {
		t.Errorf("expected error to suggest 'plan' mode, got: %s", errMsg)
	}
}

// TestLocalModels_FriendlyConnectionRefused tests server-specific guidance on connection failure.
func TestLocalModels_FriendlyConnectionRefused(t *testing.T) {
	// 1. Ollama port (11434)
	p1, _ := openai.New(openai.Config{
		BaseURL: "http://127.0.0.1:11434/v1",
		Model:   "llama3.3",
		APIKey:  "ollama",
	})
	stream1, _ := p1.Stream(context.Background(), llm.Request{Messages: []llm.Message{{Role: "user", Text: "hi"}}})
	var err1 error
	for ev := range stream1 {
		if ev.Type == llm.StreamError {
			err1 = ev.Err
		}
	}
	if err1 != nil {
		// If Ollama is not running on machine (expected in CI / test):
		if strings.Contains(err1.Error(), "connection refused") {
			if !strings.Contains(err1.Error(), "ollama serve") {
				t.Errorf("expected error to mention 'ollama serve', got: %s", err1.Error())
			}
		}
	}

	// 2. LM Studio port (1234)
	p2, _ := openai.New(openai.Config{
		BaseURL: "http://127.0.0.1:1234/v1",
		Model:   "model",
		APIKey:  "lmstudio",
	})
	stream2, _ := p2.Stream(context.Background(), llm.Request{Messages: []llm.Message{{Role: "user", Text: "hi"}}})
	var err2 error
	for ev := range stream2 {
		if ev.Type == llm.StreamError {
			err2 = ev.Err
		}
	}
	if err2 != nil {
		if strings.Contains(err2.Error(), "connection refused") {
			if !strings.Contains(err2.Error(), "LM Studio") {
				t.Errorf("expected error to mention 'LM Studio', got: %s", err2.Error())
			}
		}
	}
}
