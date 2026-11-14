package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchServerContextLength_Ollama(t *testing.T) {
	// 1. Ollama with num_ctx in parameters
	tsParams := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			http.NotFound(w, r)
			return
		}
		resp := map[string]any{
			"parameters": "temperature 0.7\nnum_ctx 8192",
			"model_info": map[string]any{
				"llama.context_length": 131072,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer tsParams.Close()

	ctx := context.Background()
	n, ok := FetchServerContextLength(ctx, "ollama", "llama3.2-params", tsParams.URL)
	if !ok || n != 8192 {
		t.Fatalf("expected 8192 from parameters num_ctx, got %d (ok=%v)", n, ok)
	}

	// 2. Ollama with only model_info context_length
	tsInfo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			http.NotFound(w, r)
			return
		}
		resp := map[string]any{
			"parameters": "temperature 0.7",
			"model_info": map[string]any{
				"qwen2.context_length": float64(32768),
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer tsInfo.Close()

	n, ok = FetchServerContextLength(ctx, "ollama", "qwen2.5-info", tsInfo.URL)
	if !ok || n != 32768 {
		t.Fatalf("expected 32768 from model_info, got %d (ok=%v)", n, ok)
	}
}

func TestFetchServerContextLength_LMStudio(t *testing.T) {
	// LM Studio reporting loaded and max context length
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/models" {
			http.NotFound(w, r)
			return
		}
		resp := map[string]any{
			"data": []map[string]any{
				{
					"id":                    "qwen2.5-coder",
					"max_context_length":    262144,
					"loaded_context_length": 81920,
				},
				{
					"id":                 "deepseek-coder",
					"max_context_length": 128000,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	ctx := context.Background()

	// Should prefer loaded_context_length (81920) over max (262144)
	n, ok := FetchServerContextLength(ctx, "lmstudio", "qwen2.5-coder", ts.URL)
	if !ok || n != 81920 {
		t.Fatalf("expected loaded_context_length 81920, got %d (ok=%v)", n, ok)
	}

	// For deepseek-coder (no loaded length), fall back to max_context_length (128000)
	n, ok = FetchServerContextLength(ctx, "lmstudio", "deepseek-coder", ts.URL)
	if !ok || n != 128000 {
		t.Fatalf("expected max_context_length 128000, got %d (ok=%v)", n, ok)
	}
}

func TestFetchServerContextLength_SecurityAndErrors(t *testing.T) {
	ctx := context.Background()

	// Hosted providers must NOT be queried
	n, ok := FetchServerContextLength(ctx, "openai", "gpt-4o", "http://127.0.0.1:1234")
	if ok || n != 0 {
		t.Fatalf("expected hosted openai to return (0, false), got %d, %v", n, ok)
	}

	// Non-loopback base URL must be rejected
	n, ok = FetchServerContextLength(ctx, "ollama", "llama3.3", "http://example.com:11434")
	if ok || n != 0 {
		t.Fatalf("expected non-loopback URL to be rejected, got %d, %v", n, ok)
	}

	// Connection failure returns (0, false) without error
	n, ok = FetchServerContextLength(ctx, "ollama", "llama3.3", "http://127.0.0.1:54321")
	if ok || n != 0 {
		t.Fatalf("expected connection failure to return (0, false), got %d, %v", n, ok)
	}
}
