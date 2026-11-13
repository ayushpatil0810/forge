package llm_test

import (
	"testing"

	"github.com/ayushpatil0810/forge/internal/llm"
	_ "github.com/ayushpatil0810/forge/internal/llm/anthropic"
	_ "github.com/ayushpatil0810/forge/internal/llm/gemini"
	_ "github.com/ayushpatil0810/forge/internal/llm/mock"
	_ "github.com/ayushpatil0810/forge/internal/llm/openai"
)

func TestNewProviderFactory(t *testing.T) {
	// Test mock provider
	pMock, err := llm.NewProvider(llm.ProviderConfig{Provider: "mock"})
	if err != nil {
		t.Fatalf("failed to create mock provider: %v", err)
	}
	if pMock.Name() != "mock" {
		t.Errorf("expected 'mock', got %q", pMock.Name())
	}

	// Test anthropic with key
	pAnthropic, err := llm.NewProvider(llm.ProviderConfig{
		Provider: "anthropic",
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create anthropic provider: %v", err)
	}
	if pAnthropic.Name() != "anthropic" {
		t.Errorf("expected 'anthropic', got %q", pAnthropic.Name())
	}

	// Test openai with key
	pOpenAI, err := llm.NewProvider(llm.ProviderConfig{
		Provider: "openai",
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create openai provider: %v", err)
	}
	if pOpenAI.Name() != "openai" {
		t.Errorf("expected 'openai', got %q", pOpenAI.Name())
	}

	// Test gemini with key
	pGemini, err := llm.NewProvider(llm.ProviderConfig{
		Provider: "gemini",
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create gemini provider: %v", err)
	}
	if pGemini.Name() != "gemini" {
		t.Errorf("expected 'gemini', got %q", pGemini.Name())
	}

	// Test unknown provider
	_, err = llm.NewProvider(llm.ProviderConfig{Provider: "unknown-vendor"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
