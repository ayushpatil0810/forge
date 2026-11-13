package gemini_test

import (
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/llm/gemini"
)

func TestMissingAPIKeyFailsFast(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	_, err := gemini.New(gemini.Config{})
	if err == nil {
		t.Fatal("expected error when GEMINI_API_KEY and GOOGLE_API_KEY are missing")
	}
	if !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Fatalf("expected error mentioning GEMINI_API_KEY, got: %v", err)
	}
}

func TestGeminiModelsCatalog(t *testing.T) {
	p, err := gemini.New(gemini.Config{APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}

	models := p.Models()
	if len(models) == 0 {
		t.Fatal("expected models in catalog")
	}

	foundDefault := false
	for _, m := range models {
		if m.ID == "gemini-2.5-pro" {
			foundDefault = true
			if m.InputPrice <= 0 || m.OutputPrice <= 0 {
				t.Errorf("gemini-2.5-pro missing pricing: %+v", m)
			}
			if m.ContextWindow != 1048576 {
				t.Errorf("expected 1048576 context window, got %d", m.ContextWindow)
			}
		}
	}
	if !foundDefault {
		t.Error("default model gemini-2.5-pro not found in catalog")
	}
}
