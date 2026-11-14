package llm

import "strings"

// ContextInfo encapsulates resolved context window details.
type ContextInfo struct {
	Window int    `json:"window"`
	Source string `json:"source"` // "override", "user_config", "server", "catalog", "unknown"
	Known  bool   `json:"known"`
}

// CatalogPriceNote is the note shown when prices come from the built-in table (Amendment B).
const CatalogPriceNote = "price from built-in table, may be out of date; set your own with /model"

// PriceInfo encapsulates resolved model pricing (USD per million tokens).
type PriceInfo struct {
	InputPrice  float64 `json:"input_price"`
	OutputPrice float64 `json:"output_price"`
	Source      string  `json:"source"` // "user_config", "catalog", "unknown"
	Known       bool    `json:"known"`
	Note        string  `json:"note,omitempty"`
}

// Global static catalog map by provider and model ID (lowercase).
var staticCatalog = map[string]map[string]ModelInfo{
	"anthropic": {
		"claude-sonnet-5-5": {ID: "claude-sonnet-5-5", Name: "Claude 3.7 Sonnet", ContextWindow: 1000000, InputPrice: 2.00, OutputPrice: 10.00},
		"claude-opus-5-5":   {ID: "claude-opus-5-5", Name: "Claude 3.5 Opus", ContextWindow: 1000000, InputPrice: 4.00, OutputPrice: 20.00},
		"claude-haiku-5-5":  {ID: "claude-haiku-5-5", Name: "Claude 3.5 Haiku", ContextWindow: 1000000, InputPrice: 0.10, OutputPrice: 0.50},
		"claude-fable-5-2":  {ID: "claude-fable-5-2", Name: "Claude 3.5 Fable", ContextWindow: 1000000, InputPrice: 8.00, OutputPrice: 40.00},
	},
	"openai": {
		"gpt-4o":      {ID: "gpt-4o", Name: "GPT-4o", ContextWindow: 128000, InputPrice: 2.50, OutputPrice: 10.00},
		"gpt-4o-mini": {ID: "gpt-4o-mini", Name: "GPT-4o Mini", ContextWindow: 128000, InputPrice: 0.15, OutputPrice: 0.60},
		"o1":          {ID: "o1", Name: "OpenAI o1", ContextWindow: 200000, InputPrice: 15.00, OutputPrice: 60.00},
		"o3-mini":     {ID: "o3-mini", Name: "OpenAI o3-mini", ContextWindow: 200000, InputPrice: 1.10, OutputPrice: 4.40},
	},
	"gemini": {
		"gemini-2.5-pro":   {ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", ContextWindow: 1048576, InputPrice: 1.25, OutputPrice: 10.00},
		"gemini-2.5-flash": {ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", ContextWindow: 1048576, InputPrice: 0.30, OutputPrice: 2.50},
	},
	"ollama": {
		"llama3.3":         {ID: "llama3.3", Name: "Llama 3.3 70B", ContextWindow: 128000, InputPrice: 0, OutputPrice: 0},
		"qwen2.5-coder:7b": {ID: "qwen2.5-coder:7b", Name: "Qwen 2.5 Coder 7B", ContextWindow: 128000, InputPrice: 0, OutputPrice: 0},
		"deepseek-r1:7b":   {ID: "deepseek-r1:7b", Name: "DeepSeek R1 Distill 7B", ContextWindow: 128000, InputPrice: 0, OutputPrice: 0},
	},
	"lmstudio": {
		"model":          {ID: "model", Name: "Loaded Model", ContextWindow: 128000, InputPrice: 0, OutputPrice: 0},
		"qwen2.5-coder":  {ID: "qwen2.5-coder", Name: "Qwen 2.5 Coder", ContextWindow: 128000, InputPrice: 0, OutputPrice: 0},
		"deepseek-coder": {ID: "deepseek-coder", Name: "DeepSeek Coder", ContextWindow: 128000, InputPrice: 0, OutputPrice: 0},
	},
	"mock": {
		"mock-model": {ID: "mock-model", Name: "Mock Model", ContextWindow: 128000, InputPrice: 0, OutputPrice: 0},
	},
}

// LookupCatalog looks up a model across known provider catalogs.
func LookupCatalog(provider, model string) (ModelInfo, bool) {
	provKey := strings.ToLower(strings.TrimSpace(provider))
	modKey := strings.ToLower(strings.TrimSpace(model))

	if models, ok := staticCatalog[provKey]; ok {
		if m, ok := models[modKey]; ok {
			return m, true
		}
	}
	// Fallback: search across all providers by model ID
	for _, models := range staticCatalog {
		if m, ok := models[modKey]; ok {
			return m, true
		}
	}
	return ModelInfo{}, false
}

// ResolveContextInfo resolves the context window using the strict precedence order:
// 1. Explicit override (--context-window flag or context.window config)
// 2. User-saved model context_window (user config)
// 3. Server-reported value
// 4. Existing catalog
// 5. Unknown
func ResolveContextInfo(provider, model string, overrideWindow, userConfigWindow, serverWindow int) ContextInfo {
	if overrideWindow > 0 {
		return ContextInfo{Window: overrideWindow, Source: "override", Known: true}
	}
	if userConfigWindow > 0 {
		return ContextInfo{Window: userConfigWindow, Source: "user_config", Known: true}
	}
	if serverWindow > 0 {
		return ContextInfo{Window: serverWindow, Source: "server", Known: true}
	}
	if info, ok := LookupCatalog(provider, model); ok && info.ContextWindow > 0 {
		return ContextInfo{Window: info.ContextWindow, Source: "catalog", Known: true}
	}
	return ContextInfo{Window: 0, Source: "unknown", Known: false}
}

// ResolvePriceInfo resolves token pricing (USD per million tokens) using the strict precedence order:
// 1. User-saved model pricing (user config)
// 2. Existing catalog (with catalog warning note)
// 3. Unknown
func ResolvePriceInfo(provider, model string, userConfigInput, userConfigOutput float64) PriceInfo {
	if userConfigInput > 0 || userConfigOutput > 0 {
		return PriceInfo{InputPrice: userConfigInput, OutputPrice: userConfigOutput, Source: "user_config", Known: true}
	}
	if info, ok := LookupCatalog(provider, model); ok {
		provKey := strings.ToLower(strings.TrimSpace(provider))
		if info.InputPrice > 0 || info.OutputPrice > 0 {
			return PriceInfo{
				InputPrice:  info.InputPrice,
				OutputPrice: info.OutputPrice,
				Source:      "catalog",
				Known:       true,
				Note:        CatalogPriceNote,
			}
		}
		if provKey == "ollama" || provKey == "lmstudio" || provKey == "mock" {
			return PriceInfo{InputPrice: 0, OutputPrice: 0, Source: "catalog", Known: true}
		}
	}
	provKey := strings.ToLower(strings.TrimSpace(provider))
	if provKey == "ollama" || provKey == "lmstudio" || provKey == "mock" {
		return PriceInfo{InputPrice: 0, OutputPrice: 0, Source: "catalog", Known: true}
	}
	return PriceInfo{InputPrice: 0, OutputPrice: 0, Source: "unknown", Known: false}
}
