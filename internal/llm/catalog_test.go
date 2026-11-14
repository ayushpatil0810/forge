package llm_test

import (
	"testing"

	"github.com/ayushpatil0810/forge/internal/llm"
)

func TestContextInfoResolverPrecedence(t *testing.T) {
	// 1. Explicit override wins over everything
	ci := llm.ResolveContextInfo("openai", "gpt-4o", 64000, 32000, 16000)
	if ci.Window != 64000 || ci.Source != "override" || !ci.Known {
		t.Fatalf("expected override window 64000, got %+v", ci)
	}

	// 2. User config wins over server and catalog
	ci = llm.ResolveContextInfo("openai", "gpt-4o", 0, 32000, 16000)
	if ci.Window != 32000 || ci.Source != "user_config" || !ci.Known {
		t.Fatalf("expected user_config window 32000, got %+v", ci)
	}

	// 3. Server reported wins over catalog
	ci = llm.ResolveContextInfo("openai", "gpt-4o", 0, 0, 16000)
	if ci.Window != 16000 || ci.Source != "server" || !ci.Known {
		t.Fatalf("expected server window 16000, got %+v", ci)
	}

	// 4. Catalog fallback
	ci = llm.ResolveContextInfo("openai", "gpt-4o", 0, 0, 0)
	if ci.Window != 128000 || ci.Source != "catalog" || !ci.Known {
		t.Fatalf("expected catalog window 128000 for gpt-4o, got %+v", ci)
	}

	// 5. Unknown model
	ci = llm.ResolveContextInfo("custom-provider", "custom-model", 0, 0, 0)
	if ci.Known || ci.Window != 0 || ci.Source != "unknown" {
		t.Fatalf("expected unknown for custom model, got %+v", ci)
	}
}

func TestPriceInfoResolverPrecedence(t *testing.T) {
	// 1. User config pricing wins over catalog
	pi := llm.ResolvePriceInfo("openai", "gpt-4o", 3.0, 12.0)
	if pi.InputPrice != 3.0 || pi.OutputPrice != 12.0 || pi.Source != "user_config" || !pi.Known {
		t.Fatalf("expected user_config pricing, got %+v", pi)
	}
	if pi.Note != "" {
		t.Fatalf("expected no note for user config pricing, got %q", pi.Note)
	}

	// 2. Catalog fallback (includes catalog note per Amendment B)
	pi = llm.ResolvePriceInfo("openai", "gpt-4o", 0, 0)
	if pi.InputPrice != 2.50 || pi.OutputPrice != 10.0 || pi.Source != "catalog" || !pi.Known {
		t.Fatalf("expected catalog pricing for gpt-4o, got %+v", pi)
	}
	if pi.Note != llm.CatalogPriceNote {
		t.Fatalf("expected catalog note %q, got %q", llm.CatalogPriceNote, pi.Note)
	}

	// 3. Local/mock model is known free (0 price, no note)
	piMock := llm.ResolvePriceInfo("mock", "mock-model", 0, 0)
	if !piMock.Known || piMock.InputPrice != 0 || piMock.OutputPrice != 0 {
		t.Fatalf("expected free known price for mock, got %+v", piMock)
	}

	// 4. Unknown model
	pi = llm.ResolvePriceInfo("custom-provider", "custom-model", 0, 0)
	if pi.Known || pi.Source != "unknown" {
		t.Fatalf("expected unknown pricing for custom model, got %+v", pi)
	}
}
