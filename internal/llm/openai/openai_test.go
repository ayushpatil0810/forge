package openai_test

import (
	"strings"
	"testing"

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

