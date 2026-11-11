package mock_test

import (
	"context"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
)

func TestMockProvider(t *testing.T) {
	p := mock.New(
		mock.Turn{
			mock.Thinking("reasoning"),
			mock.Text("hello"),
			mock.Usage(5, 2),
		},
	)

	if p.Name() != "mock" {
		t.Fatalf("want mock name, got %s", p.Name())
	}
	if len(p.Models()) == 0 {
		t.Fatal("expected at least one model in Models()")
	}

	ch, err := p.Stream(context.Background(), llm.Request{})
	if err != nil {
		t.Fatal(err)
	}

	var gotText string
	var gotThinking string
	for ev := range ch {
		if ev.Type == llm.StreamText {
			gotText += ev.Text
		}
		if ev.Type == llm.StreamThinking {
			gotThinking += ev.Text
		}
	}

	if gotText != "hello" {
		t.Fatalf("want 'hello', got %q", gotText)
	}
	if gotThinking != "reasoning" {
		t.Fatalf("want 'reasoning', got %q", gotThinking)
	}
}

func TestMockProviderHonorsCancel(t *testing.T) {
	p := mock.New(
		mock.Turn{
			mock.Text("1"),
			mock.Text("2"),
			mock.Text("3"),
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	ch, err := p.Stream(ctx, llm.Request{})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()

	select {
	case <-done:
		// success: channel was closed without hanging
	case <-time.After(2 * time.Second):
		t.Fatal("channel stream did not terminate after context cancel")
	}
}
