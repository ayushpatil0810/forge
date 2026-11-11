package anthropic_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/anthropic"
)

func TestMissingAPIKeyFailsFast(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := anthropic.New(anthropic.Config{})
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is not set")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("expected error message to mention ANTHROPIC_API_KEY, got: %v", err)
	}
}

func TestNormalStreamWithUsageAndThinking(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			"event: message_start\ndata: {\"message\":{\"usage\":{\"input_tokens\":25}}}\n\n",
			"event: content_block_delta\ndata: {\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"Analyzing code...\"}}\n\n",
			"event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello world!\"}}\n\n",
			"event: message_delta\ndata: {\"usage\":{\"output_tokens\":10}}\n\n",
			"event: message_stop\ndata: {}\n\n",
		}

		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer ts.Close()

	p, err := anthropic.New(anthropic.Config{
		APIKey:   "test-key",
		Endpoint: ts.URL,
	})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := p.Stream(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var gotText string
	var gotThinking string
	var finalUsage *llm.Usage

	for ev := range ch {
		switch ev.Type {
		case llm.StreamText:
			gotText += ev.Text
		case llm.StreamThinking:
			gotThinking += ev.Text
		case llm.StreamUsage:
			finalUsage = ev.Usage
		case llm.StreamError:
			t.Fatalf("unexpected stream error: %v", ev.Err)
		}
	}

	if gotText != "Hello world!" {
		t.Fatalf("want 'Hello world!', got %q", gotText)
	}
	if gotThinking != "Analyzing code..." {
		t.Fatalf("want 'Analyzing code...', got %q", gotThinking)
	}
	if finalUsage == nil || finalUsage.InputTokens != 25 || finalUsage.OutputTokens != 10 {
		t.Fatalf("unexpected usage: %+v", finalUsage)
	}
}

func TestMidStreamError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			"event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"Part 1\"}}\n\n",
			"event: error\ndata: {\"error\":{\"type\":\"overloaded_error\",\"message\":\"Server overloaded midway\"}}\n\n",
		}
		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer ts.Close()

	p, err := anthropic.New(anthropic.Config{APIKey: "test", Endpoint: ts.URL})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := p.Stream(context.Background(), llm.Request{})
	if err != nil {
		t.Fatal(err)
	}

	var gotError error
	for ev := range ch {
		if ev.Type == llm.StreamError {
			gotError = ev.Err
		}
	}
	if gotError == nil || !strings.Contains(gotError.Error(), "Server overloaded midway") {
		t.Fatalf("expected mid-stream error, got: %v", gotError)
	}
}

func TestRetryOn429ThenSuccess(t *testing.T) {
	var attempts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"error":{"type":"rate_limit_error","message":"rate limited"}}`, http.StatusTooManyRequests)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"Recovered\"}}\n\n"))
	}))
	defer ts.Close()

	p, err := anthropic.New(anthropic.Config{APIKey: "test", Endpoint: ts.URL})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := p.Stream(context.Background(), llm.Request{})
	if err != nil {
		t.Fatal(err)
	}

	var gotText string
	for ev := range ch {
		if ev.Type == llm.StreamText {
			gotText += ev.Text
		}
	}

	if attempts.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts.Load())
	}
	if gotText != "Recovered" {
		t.Fatalf("expected 'Recovered', got %q", gotText)
	}
}

func TestNoRetryOn401(t *testing.T) {
	var attempts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, `{"error":{"type":"authentication_error","message":"invalid api key"}}`, http.StatusUnauthorized)
	}))
	defer ts.Close()

	p, err := anthropic.New(anthropic.Config{APIKey: "bad-key", Endpoint: ts.URL})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := p.Stream(context.Background(), llm.Request{})
	if err != nil {
		t.Fatal(err)
	}

	var gotError error
	for ev := range ch {
		if ev.Type == llm.StreamError {
			gotError = ev.Err
		}
	}

	if attempts.Load() != 1 {
		t.Fatalf("expected exactly 1 attempt (no retry on 401), got %d", attempts.Load())
	}
	if gotError == nil || !strings.Contains(gotError.Error(), "authentication_error") {
		t.Fatalf("expected auth error, got: %v", gotError)
	}
}

func TestCancellationDuringBackoff(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "overloaded", 529)
	}))
	defer ts.Close()

	p, err := anthropic.New(anthropic.Config{APIKey: "test", Endpoint: ts.URL})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Stream(ctx, llm.Request{})
	if err != nil {
		t.Fatal(err)
	}

	// Cancel context quickly while backoff would be sleeping
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()

	select {
	case <-done:
		// Succeeded in terminating on cancel without waiting full backoff
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not cancel promptly during backoff")
	}
}

func TestModelsCatalog(t *testing.T) {
	p, err := anthropic.New(anthropic.Config{APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	models := p.Models()
	if len(models) < 3 {
		t.Fatalf("expected at least 3 models in catalog, got %d", len(models))
	}
	for _, m := range models {
		if m.ID == "" || m.Name == "" || m.ContextWindow <= 0 {
			t.Fatalf("invalid model info: %+v", m)
		}
	}
}
