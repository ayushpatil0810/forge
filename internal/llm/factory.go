package llm

import (
	"fmt"
	"strings"
	"sync"
)

// ProviderConfig holds settings to instantiate a Provider.
type ProviderConfig struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}

// ProviderFactory is a function that creates a Provider from ProviderConfig.
type ProviderFactory func(cfg ProviderConfig) (Provider, error)

var (
	factoriesMu sync.RWMutex
	factories   = map[string]ProviderFactory{}
)

// RegisterProvider registers a provider constructor by name.
func RegisterProvider(name string, factory ProviderFactory) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	factories[strings.ToLower(name)] = factory
}

// NewProvider instantiates the configured Provider.
// Defaults to "anthropic" if Provider is not specified.
func NewProvider(cfg ProviderConfig) (Provider, error) {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()

	name := strings.ToLower(cfg.Provider)
	if name == "" {
		name = "anthropic"
	}

	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q; supported providers: anthropic, openai, gemini, mock", cfg.Provider)
	}

	return factory(cfg)
}
