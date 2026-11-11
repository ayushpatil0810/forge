package llm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/anthropic"
	"github.com/ayushpatil0810/forge/internal/llm/gemini"
	"github.com/ayushpatil0810/forge/internal/llm/openai"
)

type ProviderWireFixtures interface {
	Name() string
	TextStream() []string
	SingleToolCall(toolName, argsJSON string) []string
	MultipleToolCalls(call1Name, call1Args, call2Name, call2Args string) []string
	SplitToolCallChunks(toolName, argChunk1, argChunk2 string) []string
	UsageStream(inTokens, outTokens int) []string
	MidStreamError() []string
	RateLimit429Body() string
	Unauthorized401Body() string
	CreateProvider(serverURL string) (llm.Provider, error)
}

func escapeJSONString(s string) string {
	b, _ := json.Marshal(s)
	// Remove outer quotes
	return string(b[1 : len(b)-1])
}

// -------------------------------------------------------------
// Anthropic Fixtures
// -------------------------------------------------------------

type AnthropicFixtures struct{}

func (AnthropicFixtures) Name() string { return "Anthropic" }

func (AnthropicFixtures) TextStream() []string {
	return []string{
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello \"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"World!\"}}\n\n",
	}
}

func (AnthropicFixtures) SingleToolCall(name, args string) []string {
	return []string{
		fmt.Sprintf("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_ant_1\",\"name\":%q}}\n\n", name),
		fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%q}}\n\n", args),
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n",
	}
}

func (AnthropicFixtures) MultipleToolCalls(n1, a1, n2, a2 string) []string {
	return []string{
		fmt.Sprintf("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_ant_1\",\"name\":%q}}\n\n", n1),
		fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%q}}\n\n", a1),
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n",
		fmt.Sprintf("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":2,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_ant_2\",\"name\":%q}}\n\n", n2),
		fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":2,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%q}}\n\n", a2),
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":2}\n\n",
	}
}

func (AnthropicFixtures) SplitToolCallChunks(name, c1, c2 string) []string {
	return []string{
		fmt.Sprintf("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_ant_split\",\"name\":%q}}\n\n", name),
		fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%q}}\n\n", c1),
		fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%q}}\n\n", c2),
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n",
	}
}

func (AnthropicFixtures) UsageStream(in, out int) []string {
	return []string{
		fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":%d}}}\n\n", in),
		fmt.Sprintf("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":%d}}\n\n", out),
	}
}

func (AnthropicFixtures) MidStreamError() []string {
	return []string{
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Beginning \"}}\n\n",
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Anthropic stream overloaded\"}}\n\n",
	}
}

func (AnthropicFixtures) RateLimit429Body() string {
	return `{"type":"error","error":{"type":"rate_limit_error","message":"rate limit exceeded"}}`
}

func (AnthropicFixtures) Unauthorized401Body() string {
	return `{"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`
}

func (AnthropicFixtures) CreateProvider(serverURL string) (llm.Provider, error) {
	return anthropic.New(anthropic.Config{
		APIKey:   "test-key",
		Endpoint: serverURL,
	})
}

// -------------------------------------------------------------
// OpenAI Fixtures
// -------------------------------------------------------------

type OpenAIFixtures struct{}

func (OpenAIFixtures) Name() string { return "OpenAI" }

func (OpenAIFixtures) TextStream() []string {
	return []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"World!\"}}]}\n\n",
		"data: [DONE]\n\n",
	}
}

func (OpenAIFixtures) SingleToolCall(name, args string) []string {
	return []string{
		fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_oai_1\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]}}]}\n\n", name, args),
		"data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n",
		"data: [DONE]\n\n",
	}
}

func (OpenAIFixtures) MultipleToolCalls(n1, a1, n2, a2 string) []string {
	return []string{
		fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_oai_1\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}},{\"index\":1,\"id\":\"call_oai_2\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]}}]}\n\n", n1, a1, n2, a2),
		"data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n",
		"data: [DONE]\n\n",
	}
}

func (OpenAIFixtures) SplitToolCallChunks(name, c1, c2 string) []string {
	return []string{
		fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_oai_split\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]}}]}\n\n", name, c1),
		fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":%q}}]}}]}\n\n", c2),
		"data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n",
		"data: [DONE]\n\n",
	}
}

func (OpenAIFixtures) UsageStream(in, out int) []string {
	return []string{
		fmt.Sprintf("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d}}\n\n", in, out),
		"data: [DONE]\n\n",
	}
}

func (OpenAIFixtures) MidStreamError() []string {
	return []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Beginning \"}}]}\n\n",
		"data: {\"error\":{\"message\":\"OpenAI stream error midstream\",\"type\":\"server_error\"}}\n\n",
	}
}

func (OpenAIFixtures) RateLimit429Body() string {
	return `{"error":{"message":"Rate limit reached","type":"insufficient_quota"}}`
}

func (OpenAIFixtures) Unauthorized401Body() string {
	return `{"error":{"message":"Incorrect API key","type":"invalid_request_error"}}`
}

func (OpenAIFixtures) CreateProvider(serverURL string) (llm.Provider, error) {
	return openai.New(openai.Config{
		APIKey:  "test-key",
		BaseURL: serverURL,
	})
}

// -------------------------------------------------------------
// Gemini Fixtures
// -------------------------------------------------------------

type GeminiFixtures struct{}

func (GeminiFixtures) Name() string { return "Gemini" }

func (GeminiFixtures) TextStream() []string {
	return []string{
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hello \"}]}}]}\n\n",
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"World!\"}]}}]}\n\n",
	}
}

func (GeminiFixtures) SingleToolCall(name, args string) []string {
	return []string{
		fmt.Sprintf("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":%q,\"args\":%s}}]}}]}\n\n", name, args),
	}
}

func (GeminiFixtures) MultipleToolCalls(n1, a1, n2, a2 string) []string {
	return []string{
		fmt.Sprintf("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":%q,\"args\":%s}},{\"functionCall\":{\"name\":%q,\"args\":%s}}]}}]}\n\n", n1, a1, n2, a2),
	}
}

func (GeminiFixtures) SplitToolCallChunks(name, c1, c2 string) []string {
	// Gemini sends structured JSON objects for args; combined here to satisfy the contract
	var combined map[string]any
	raw := c1 + c2
	_ = json.Unmarshal([]byte(raw), &combined)
	b, _ := json.Marshal(combined)
	return []string{
		fmt.Sprintf("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":%q,\"args\":%s}}]}}]}\n\n", name, string(b)),
	}
}

func (GeminiFixtures) UsageStream(in, out int) []string {
	return []string{
		fmt.Sprintf("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"\"}]}}],\"usageMetadata\":{\"promptTokenCount\":%d,\"candidatesTokenCount\":%d}}\n\n", in, out),
	}
}

func (GeminiFixtures) MidStreamError() []string {
	return []string{
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Beginning \"}]}}]}\n\n",
		"data: {\"error\":{\"code\":500,\"message\":\"Gemini internal error midstream\"}}\n\n",
	}
}

func (GeminiFixtures) RateLimit429Body() string {
	return `{"error":{"code":429,"message":"Resource exhausted","status":"RESOURCE_EXHAUSTED"}}`
}

func (GeminiFixtures) Unauthorized401Body() string {
	return `{"error":{"code":401,"message":"API key not valid","status":"UNAUTHENTICATED"}}`
}

func (GeminiFixtures) CreateProvider(serverURL string) (llm.Provider, error) {
	return gemini.New(gemini.Config{
		APIKey:   "test-key",
		Endpoint: serverURL,
	})
}

// -------------------------------------------------------------
// Shared Contract Test Suite Runner
// -------------------------------------------------------------

func runContractSuite(t *testing.T, fix ProviderWireFixtures) {
	t.Run(fix.Name()+"/TextStream", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, chunk := range fix.TextStream() {
				fmt.Fprint(w, chunk)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}))
		defer ts.Close()

		p, err := fix.CreateProvider(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		ch, err := p.Stream(context.Background(), llm.Request{})
		if err != nil {
			t.Fatal(err)
		}

		var collected strings.Builder
		for ev := range ch {
			if ev.Type == llm.StreamText {
				collected.WriteString(ev.Text)
			}
		}

		if collected.String() != "Hello World!" {
			t.Fatalf("expected 'Hello World!', got %q", collected.String())
		}
	})

	t.Run(fix.Name()+"/SingleToolCall", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, chunk := range fix.SingleToolCall("read_file", `{"path":"main.go"}`) {
				fmt.Fprint(w, chunk)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}))
		defer ts.Close()

		p, err := fix.CreateProvider(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		ch, err := p.Stream(context.Background(), llm.Request{})
		if err != nil {
			t.Fatal(err)
		}

		var calls []llm.ToolCall
		for ev := range ch {
			if ev.Type == llm.StreamToolCall && ev.ToolCall != nil {
				calls = append(calls, *ev.ToolCall)
			}
		}

		if len(calls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(calls))
		}
		if calls[0].Name != "read_file" {
			t.Fatalf("expected name 'read_file', got %q", calls[0].Name)
		}
		var parsed map[string]any
		if err := json.Unmarshal(calls[0].Input, &parsed); err != nil {
			t.Fatalf("tool call input was not valid JSON: %s (%v)", string(calls[0].Input), err)
		}
		if parsed["path"] != "main.go" {
			t.Fatalf("expected path 'main.go', got %v", parsed["path"])
		}
	})

	t.Run(fix.Name()+"/MultipleToolCalls", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, chunk := range fix.MultipleToolCalls("list_dir", `{"path":"."}`, "read_file", `{"path":"go.mod"}`) {
				fmt.Fprint(w, chunk)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}))
		defer ts.Close()

		p, err := fix.CreateProvider(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		ch, err := p.Stream(context.Background(), llm.Request{})
		if err != nil {
			t.Fatal(err)
		}

		var calls []llm.ToolCall
		for ev := range ch {
			if ev.Type == llm.StreamToolCall && ev.ToolCall != nil {
				calls = append(calls, *ev.ToolCall)
			}
		}

		if len(calls) != 2 {
			t.Fatalf("expected 2 tool calls, got %d", len(calls))
		}
		if calls[0].Name != "list_dir" || calls[1].Name != "read_file" {
			t.Fatalf("unexpected call names: %v", calls)
		}
	})

	t.Run(fix.Name()+"/SplitToolCallChunks", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, chunk := range fix.SplitToolCallChunks("read_file", `{"path":`, `"main.go"}`) {
				fmt.Fprint(w, chunk)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}))
		defer ts.Close()

		p, err := fix.CreateProvider(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		ch, err := p.Stream(context.Background(), llm.Request{})
		if err != nil {
			t.Fatal(err)
		}

		var calls []llm.ToolCall
		for ev := range ch {
			if ev.Type == llm.StreamToolCall && ev.ToolCall != nil {
				calls = append(calls, *ev.ToolCall)
			}
		}

		if len(calls) != 1 {
			t.Fatalf("expected 1 assembled tool call, got %d", len(calls))
		}
		var parsed map[string]any
		if err := json.Unmarshal(calls[0].Input, &parsed); err != nil {
			t.Fatalf("assembled input was invalid JSON: %s (%v)", string(calls[0].Input), err)
		}
		if parsed["path"] != "main.go" {
			t.Fatalf("expected path 'main.go', got %v", parsed["path"])
		}
	})

	t.Run(fix.Name()+"/UsageReporting", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, chunk := range fix.UsageStream(120, 45) {
				fmt.Fprint(w, chunk)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}))
		defer ts.Close()

		p, err := fix.CreateProvider(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		ch, err := p.Stream(context.Background(), llm.Request{})
		if err != nil {
			t.Fatal(err)
		}

		var lastUsage *llm.Usage
		for ev := range ch {
			if ev.Type == llm.StreamUsage && ev.Usage != nil {
				lastUsage = ev.Usage
			}
		}

		if lastUsage == nil {
			t.Fatal("expected usage event, got none")
		}
		if lastUsage.InputTokens != 120 || lastUsage.OutputTokens != 45 {
			t.Fatalf("expected 120 in / 45 out, got %+v", lastUsage)
		}
	})

	t.Run(fix.Name()+"/MidStreamError", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, chunk := range fix.MidStreamError() {
				fmt.Fprint(w, chunk)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}))
		defer ts.Close()

		p, err := fix.CreateProvider(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		ch, err := p.Stream(context.Background(), llm.Request{})
		if err != nil {
			t.Fatal(err)
		}

		var receivedError error
		for ev := range ch {
			if ev.Type == llm.StreamError {
				receivedError = ev.Err
			}
		}

		if receivedError == nil {
			t.Fatal("expected mid-stream error event, got none")
		}
	})

	t.Run(fix.Name()+"/RetryOn429", func(t *testing.T) {
		var attempts atomic.Int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			att := attempts.Add(1)
			if att == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprint(w, fix.RateLimit429Body())
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, chunk := range fix.TextStream() {
				fmt.Fprint(w, chunk)
			}
		}))
		defer ts.Close()

		p, err := fix.CreateProvider(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		ch, err := p.Stream(context.Background(), llm.Request{})
		if err != nil {
			t.Fatal(err)
		}

		var collected strings.Builder
		for ev := range ch {
			if ev.Type == llm.StreamText {
				collected.WriteString(ev.Text)
			}
		}

		if attempts.Load() < 2 {
			t.Fatalf("expected at least 2 attempts (retry), got %d", attempts.Load())
		}
		if collected.String() != "Hello World!" {
			t.Fatalf("expected 'Hello World!' after retry, got %q", collected.String())
		}
	})

	t.Run(fix.Name()+"/NoRetryOn401", func(t *testing.T) {
		var attempts atomic.Int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, fix.Unauthorized401Body())
		}))
		defer ts.Close()

		p, err := fix.CreateProvider(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		ch, err := p.Stream(context.Background(), llm.Request{})
		if err != nil {
			t.Fatal(err)
		}

		var receivedError error
		for ev := range ch {
			if ev.Type == llm.StreamError {
				receivedError = ev.Err
			}
		}

		if receivedError == nil {
			t.Fatal("expected error on 401 unauthorized, got none")
		}
		if attempts.Load() != 1 {
			t.Fatalf("expected exactly 1 attempt with no retry, got %d", attempts.Load())
		}
	})

	t.Run(fix.Name()+"/CancelActiveStream", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for i := 0; i < 50; i++ {
				for _, chunk := range fix.TextStream() {
					fmt.Fprint(w, chunk)
					if f, ok := w.(http.Flusher); ok {
						f.Flush()
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
		}))
		defer ts.Close()

		p, err := fix.CreateProvider(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		ch, err := p.Stream(ctx, llm.Request{})
		if err != nil {
			t.Fatal(err)
		}

		// Read first token, then cancel immediately
		<-ch
		cancel()

		// Channel must close promptly
		select {
		case <-time.After(500 * time.Millisecond):
			t.Fatal("stream did not stop immediately after cancellation")
		case _, ok := <-ch:
			// Drain remaining buffered if any, until closed
			for ok {
				_, ok = <-ch
			}
		}
	})

	t.Run(fix.Name()+"/CancelDuringBackoff", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, fix.RateLimit429Body())
		}))
		defer ts.Close()

		p, err := fix.CreateProvider(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		start := time.Now()
		go func() {
			time.Sleep(30 * time.Millisecond)
			cancel()
		}()

		ch, err := p.Stream(ctx, llm.Request{})
		if err != nil {
			t.Fatal(err)
		}

		for range ch {
		}

		if time.Since(start) > 250*time.Millisecond {
			t.Fatalf("cancellation during backoff took too long: %v", time.Since(start))
		}
	})
}

// -------------------------------------------------------------
// Test Entrypoint Running Against All Three Providers
// -------------------------------------------------------------

func TestProviderContract_Anthropic(t *testing.T) {
	runContractSuite(t, AnthropicFixtures{})
}

func TestProviderContract_OpenAI(t *testing.T) {
	runContractSuite(t, OpenAIFixtures{})
}

func TestProviderContract_Gemini(t *testing.T) {
	runContractSuite(t, GeminiFixtures{})
}

