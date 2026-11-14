package main

import (
	"context"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/llm"
)

func TestOpenInteractiveProviderWithoutKeyOpensSetup(t *testing.T) {
	p, wizard, err := openInteractiveProvider("anthropic", "claude-sonnet-5-5", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !wizard {
		t.Fatal("expected the setup wizard when no API key is stored")
	}
	if p.Name() != "unconfigured" {
		t.Fatalf("expected a placeholder provider, got %s", p.Name())
	}
	_, err = p.Stream(context.Background(), llm.Request{})
	if err == nil {
		t.Fatal("expected the placeholder to refuse a turn")
	}
	if strings.Contains(err.Error(), "ANTHROPIC_API_KEY") || strings.Contains(err.Error(), "environment variable") {
		t.Fatalf("first launch must not demand an environment variable, got %v", err)
	}
}

func TestOpenInteractiveProviderWithKeySkipsSetup(t *testing.T) {
	p, wizard, err := openInteractiveProvider("anthropic", "claude-sonnet-5-5", "", "sk-test", false)
	if err != nil {
		t.Fatal(err)
	}
	if wizard {
		t.Fatal("a stored key should open the chat, not setup")
	}
	if p.Name() != "anthropic" {
		t.Fatalf("expected anthropic, got %s", p.Name())
	}
}

func TestLocalProviderDoesNotRequireAKey(t *testing.T) {
	p, wizard, err := openInteractiveProvider("ollama", "llama3.3", "http://127.0.0.1:11434", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if wizard {
		t.Fatal("ollama should not be blocked on an API key")
	}
	if p == nil || p.Name() == "unconfigured" {
		t.Fatalf("expected a local provider, got %#v", p)
	}
}
