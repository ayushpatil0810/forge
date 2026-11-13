package openai_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/openai"
)

func TestMissingAPIKeyFailsFast(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, err := openai.New(openai.Config{})
	if err == nil {
		t.Fatal("expected error when OPENAI_API_KEY is missing")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("expected error mentioning OPENAI_API_KEY, got: %v", err)
	}
}

func TestOpenAIModelsCatalog(t *testing.T) {
	p, err := openai.New(openai.Config{APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}

	models := p.Models()
	if len(models) == 0 {
		t.Fatal("expected models in catalog")
	}

	foundDefault := false
	for _, m := range models {
		if m.ID == "gpt-4o" {
			foundDefault = true
			if m.InputPrice <= 0 || m.OutputPrice <= 0 {
				t.Errorf("gpt-4o missing pricing: %+v", m)
			}
			if m.ContextWindow != 128000 {
				t.Errorf("expected 128000 context window, got %d", m.ContextWindow)
			}
		}
	}
	if !foundDefault {
		t.Error("default model gpt-4o not found in catalog")
	}
}

func TestOpenAIProviderWithCustomBaseURL_Ollama(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	var receivedAuth string
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedPath = r.URL.Path

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "flusher not supported", 500)
			return
		}

		// SSE chunk from Ollama / OpenAI proxy
		chunk := `data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello from Ollama local model!"}}],"usage":{"prompt_tokens":10,"completion_tokens":8}}`
		_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	// Initialize provider with custom baseURL (no API key provided)
	p, err := openai.New(openai.Config{
		BaseURL: server.URL,
		Model:   "llama3.2:latest",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("expected New() to succeed with custom BaseURL, got: %v", err)
	}

	ch, err := p.Stream(context.Background(), llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Text: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var sb strings.Builder
	for ev := range ch {
		if ev.Type == llm.StreamText {
			sb.WriteString(ev.Text)
		}
	}

	if got := sb.String(); got != "Hello from Ollama local model!" {
		t.Fatalf("unexpected stream output: %q", got)
	}

	if receivedPath != "/v1/chat/completions" {
		t.Fatalf("expected request path /v1/chat/completions, got %s", receivedPath)
	}

	if receivedAuth == "" {
		t.Fatal("expected non-empty Authorization header")
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty URL returns default OpenAI endpoint",
			input:    "",
			expected: "https://api.openai.com/v1",
		},
		{
			name:     "whitespace only returns default OpenAI endpoint",
			input:    "   ",
			expected: "https://api.openai.com/v1",
		},
		{
			name:     "Ollama preset without path appends /v1",
			input:    "http://localhost:11434",
			expected: "http://localhost:11434/v1",
		},
		{
			name:     "Ollama preset with trailing slash appends /v1",
			input:    "http://127.0.0.1:11434/",
			expected: "http://127.0.0.1:11434/v1",
		},
		{
			name:     "LM Studio preset without path appends /v1",
			input:    "http://localhost:1234",
			expected: "http://localhost:1234/v1",
		},
		{
			name:     "LM Studio preset with /v1 stays intact",
			input:    "http://127.0.0.1:1234/v1",
			expected: "http://127.0.0.1:1234/v1",
		},
		{
			name:     "Gemini OpenAI-compatible endpoint with trailing slash preserved verbatim",
			input:    "https://generativelanguage.googleapis.com/v1beta/openai/",
			expected: "https://generativelanguage.googleapis.com/v1beta/openai",
		},
		{
			name:     "Custom API path .../api/v1 preserved verbatim",
			input:    "http://my-proxy:8080/api/v1",
			expected: "http://my-proxy:8080/api/v1",
		},
		{
			name:     "Custom API path .../api/v1/ trailing slash trimmed",
			input:    "http://my-proxy:8080/api/v1/",
			expected: "http://my-proxy:8080/api/v1",
		},
		{
			name:     "Custom subpath .../custom/v2 preserved verbatim",
			input:    "https://api.example.com/custom/v2",
			expected: "https://api.example.com/custom/v2",
		},
		{
			name:     "Host with empty path appends /v1",
			input:    "http://custom-host:9000",
			expected: "http://custom-host:9000/v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := openai.NormalizeBaseURL(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeBaseURL(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}
