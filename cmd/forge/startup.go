package main

import (
	"context"
	"errors"
	"strings"

	"github.com/ayushpatil0810/forge/internal/llm"
)

// unconfiguredProvider lets the TUI open before a model is chosen. It never
// contacts a provider and never asks for an environment variable.
type unconfiguredProvider struct{}

func (unconfiguredProvider) Name() string { return "unconfigured" }

func (unconfiguredProvider) Models() []llm.ModelInfo { return nil }

func (unconfiguredProvider) Stream(context.Context, llm.Request) (<-chan llm.StreamEvent, error) {
	return nil, errors.New("no model configured; type /settings to choose a provider and model")
}

func providerNeedsAPIKey(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ollama", "lmstudio", "mock":
		return false
	default:
		return true
	}
}

// openInteractiveProvider starts the chat UI even when no API key is stored.
// A missing key for a hosted provider opens the setup wizard instead of
// exiting. Headless runs do not use this path.
func openInteractiveProvider(name, model, baseURL, apiKey string, missingKey bool) (llm.Provider, bool, error) {
	if missingKey && providerNeedsAPIKey(name) {
		return unconfiguredProvider{}, true, nil
	}
	p, err := llm.NewProvider(llm.ProviderConfig{
		Provider: name,
		Model:    model,
		BaseURL:  baseURL,
		APIKey:   apiKey,
	})
	if err != nil {
		return nil, false, err
	}
	return p, false, nil
}
