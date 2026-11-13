// Package openai implements the llm.Provider interface for OpenAI models
// (and OpenAI-compatible proxies such as Ollama, vLLM, and OpenRouter)
// using Chat Completions (/v1/chat/completions) with SSE streaming.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/retry"
)

// Source documentation:
// - Models & pricing: https://openai.com/api/pricing/
const (
	defaultBaseURL = "https://api.openai.com/v1"
	// Default model is gpt-4o: flagship multimodal model with strong reasoning,
	// 128k context window, and fast inference ($2.50/M in, $10.00/M out).
	defaultModel   = "gpt-4o"
	maxRetries     = 3
	initialBackoff = 300 * time.Millisecond
	maxBackoff     = 8 * time.Second
)

// Active OpenAI models per official documentation.
// Retired legacy models (gpt-3.5-turbo, gpt-4-turbo) have been removed.
var catalog = []llm.ModelInfo{
	{
		ID:            "gpt-4o",
		Name:          "GPT-4o",
		ContextWindow: 128000,
		InputPrice:    2.50,
		OutputPrice:   10.00,
	},
	{
		ID:            "gpt-4o-mini",
		Name:          "GPT-4o Mini",
		ContextWindow: 128000,
		InputPrice:    0.15,
		OutputPrice:   0.60,
	},
	{
		ID:            "o1",
		Name:          "o1",
		ContextWindow: 200000,
		InputPrice:    15.00,
		OutputPrice:   60.00,
	},
	{
		ID:            "o3-mini",
		Name:          "o3-mini",
		ContextWindow: 200000,
		InputPrice:    1.10,
		OutputPrice:   4.40,
	},
}

func init() {
	llm.RegisterProvider("openai", func(cfg llm.ProviderConfig) (llm.Provider, error) {
		return New(Config{
			APIKey:  cfg.APIKey,
			Model:   cfg.Model,
			BaseURL: cfg.BaseURL,
		})
	})

	llm.RegisterProvider("ollama", func(cfg llm.ProviderConfig) (llm.Provider, error) {
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "http://127.0.0.1:11434/v1"
		}
		model := cfg.Model
		if model == "" {
			model = "llama3.3"
		}
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = "ollama"
		}
		return New(Config{
			APIKey:  apiKey,
			Model:   model,
			BaseURL: baseURL,
		})
	})

	llm.RegisterProvider("lmstudio", func(cfg llm.ProviderConfig) (llm.Provider, error) {
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "http://127.0.0.1:1234/v1"
		}
		model := cfg.Model
		if model == "" {
			model = "model"
		}
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = "lm-studio"
		}
		return New(Config{
			APIKey:  apiKey,
			Model:   model,
			BaseURL: baseURL,
		})
	})
}

// Config holds OpenAI provider configuration options.
type Config struct {
	APIKey  string
	Model   string
	BaseURL string
	Client  *http.Client
}

// NormalizeBaseURL normalizes an OpenAI-compatible base URL.
// Per specification, it appends /v1 only for built-in local presets (ollama, lmstudio)
// or when the URL has an empty path. All other custom base URLs (e.g. Gemini's
// .../v1beta/openai or proxies with custom endpoints like .../api/v1) are preserved
// verbatim, apart from trimming trailing slashes.
func NormalizeBaseURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return defaultBaseURL
	}
	trimmed := strings.TrimRight(rawURL, "/")
	u, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}

	isPreset := strings.Contains(u.Host, "11434") || strings.Contains(u.Host, "1234")

	if u.Path == "" || u.Path == "/" || (isPreset && !strings.HasSuffix(trimmed, "/v1")) {
		if !strings.HasSuffix(trimmed, "/v1") {
			return trimmed + "/v1"
		}
		return trimmed
	}

	return trimmed
}

// Provider implements llm.Provider for OpenAI.
type Provider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// New creates a new OpenAI provider.
// Fails fast with a friendly error if OPENAI_API_KEY is not set (unless a custom BaseURL like Ollama is configured).
func New(cfg Config) (*Provider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("OPENAI_BASE_URL")
	}
	baseURL = NormalizeBaseURL(baseURL)

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	// For local OpenAI-compatible proxies (such as Ollama or LocalAI),
	// allow running without requiring a paid API key if a custom BaseURL is set.
	if strings.TrimSpace(apiKey) == "" {
		if baseURL != defaultBaseURL {
			apiKey = "ollama"
		} else {
			return nil, errors.New("openai: OPENAI_API_KEY environment variable is required; please set it or run 'forge auth login openai'")
		}
	}

	model := cfg.Model
	if model == "" {
		model = defaultModel
	}

	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}

	return &Provider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client:  client,
	}, nil
}

func (p *Provider) Name() string {
	return "openai"
}

func (p *Provider) Models() []llm.ModelInfo {
	return catalog
}

func isReasoningModel(model string) bool {
	m := strings.ToLower(model)
	return strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") || strings.Contains(m, "astra")
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIRequest struct {
	Model               string              `json:"model"`
	Messages            []openAIMessage     `json:"messages"`
	Tools               []openAITool        `json:"tools,omitempty"`
	Stream              bool                `json:"stream"`
	StreamOptions       openAIStreamOptions `json:"stream_options"`
	MaxTokens           int                 `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                 `json:"max_completion_tokens,omitempty"`
}

func (p *Provider) calculateCost(usage *llm.Usage) float64 {
	for _, m := range catalog {
		if m.ID == p.model {
			return (float64(usage.InputTokens)*m.InputPrice + float64(usage.OutputTokens)*m.OutputPrice) / 1_000_000
		}
	}
	return 0
}

func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	bodyObj := openAIRequest{
		Model:         p.model,
		Stream:        true,
		StreamOptions: openAIStreamOptions{IncludeUsage: true},
	}

	if isReasoningModel(p.model) {
		bodyObj.MaxCompletionTokens = req.MaxTokens
	} else {
		bodyObj.MaxTokens = req.MaxTokens
	}

	if req.System != "" {
		systemRole := "system"
		if isReasoningModel(p.model) {
			systemRole = "developer"
		}
		sysText := req.System
		bodyObj.Messages = append(bodyObj.Messages, openAIMessage{
			Role:    systemRole,
			Content: &sysText,
		})
	}

	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleUser:
			text := m.Text
			bodyObj.Messages = append(bodyObj.Messages, openAIMessage{
				Role:    "user",
				Content: &text,
			})
		case llm.RoleAssistant:
			var contentPtr *string
			if m.Text != "" {
				text := m.Text
				contentPtr = &text
			}
			var tcList []openAIToolCall
			for _, tc := range m.ToolCalls {
				var item openAIToolCall
				item.ID = tc.ID
				item.Type = "function"
				item.Function.Name = tc.Name
				item.Function.Arguments = string(tc.Input)
				tcList = append(tcList, item)
			}
			bodyObj.Messages = append(bodyObj.Messages, openAIMessage{
				Role:      "assistant",
				Content:   contentPtr,
				ToolCalls: tcList,
			})
		case llm.RoleTool:
			text := m.Text
			bodyObj.Messages = append(bodyObj.Messages, openAIMessage{
				Role:       "tool",
				Content:    &text,
				ToolCallID: m.ToolCallID,
			})
		}
	}

	for _, t := range req.Tools {
		bodyObj.Tools = append(bodyObj.Tools, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		})
	}

	bodyBytes, err := json.Marshal(bodyObj)
	if err != nil {
		return nil, fmt.Errorf("marshaling openai request: %w", err)
	}

	ch := make(chan llm.StreamEvent, 32)
	go p.streamWithRetry(ctx, bodyBytes, ch)
	return ch, nil
}

func (p *Provider) streamWithRetry(ctx context.Context, bodyBytes []byte, ch chan<- llm.StreamEvent) {
	defer close(ch)

	retryCfg := retry.Config{
		MaxRetries:     maxRetries,
		InitialBackoff: initialBackoff,
		MaxBackoff:     maxBackoff,
	}

	endpoint := p.baseURL + "/chat/completions"

	resp, err := retry.Do(ctx, retryCfg, func(opCtx context.Context) (*http.Response, bool, time.Duration, error) {
		httpReq, reqErr := http.NewRequestWithContext(opCtx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, false, 0, reqErr
		}

		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		res, doErr := p.client.Do(httpReq)
		if doErr != nil {
			if opCtx.Err() != nil {
				return nil, false, 0, opCtx.Err()
			}
			errStr := strings.ToLower(doErr.Error())
			if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "connectex") || strings.Contains(errStr, "actively refused") {
				if strings.Contains(p.baseURL, "11434") {
					return nil, false, 0, fmt.Errorf("connection refused: Ollama does not appear to be running at %s. Is Ollama running? Start it with 'ollama serve'", p.baseURL)
				}
				if strings.Contains(p.baseURL, "1234") {
					return nil, false, 0, fmt.Errorf("connection refused: LM Studio does not appear to be running at %s. Start the local server in LM Studio under Developer > Local Server", p.baseURL)
				}
				if strings.Contains(p.baseURL, "127.0.0.1") || strings.Contains(p.baseURL, "localhost") {
					return nil, false, 0, fmt.Errorf("connection refused: local model server is not running at %s; ensure your local LLM server is started", p.baseURL)
				}
			}
			return nil, true, 0, fmt.Errorf("openai connection error: %w", doErr)
		}

		if res.StatusCode == http.StatusOK {
			return res, false, 0, nil
		}

		defer func() { _ = res.Body.Close() }()
		respBody, _ := io.ReadAll(res.Body)

		isRetryable := retry.IsRetryableStatus(res.StatusCode)
		retryAfter := retry.ParseRetryAfter(res.Header.Get("Retry-After"))

		var errMsg string
		var errResp struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			errMsg = fmt.Sprintf("openai API error (%d %s): %s", res.StatusCode, errResp.Error.Type, errResp.Error.Message)
		} else {
			errMsg = fmt.Sprintf("openai API error (%d): %s", res.StatusCode, strings.TrimSpace(string(respBody)))
		}

		// Tool calling rejection handling (fail fast, non-looping, suggest plan mode)
		if res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusUnprocessableEntity {
			lowerMsg := strings.ToLower(errMsg)
			if strings.Contains(lowerMsg, "tool") || strings.Contains(lowerMsg, "function") || strings.Contains(lowerMsg, "not support") || strings.Contains(lowerMsg, "unrecognized request argument") {
				return nil, false, 0, fmt.Errorf("model %q does not support tool calling: %s. Use a tool-capable model (e.g. qwen2.5-coder, llama3.3) or run with 'plan' mode (which does not execute tools)", p.model, errMsg)
			}
		}

		return nil, isRetryable, retryAfter, errors.New(errMsg)
	})

	if err != nil {
		select {
		case ch <- llm.StreamEvent{Type: llm.StreamError, Err: err}:
		case <-ctx.Done():
		}
		return
	}

	defer func() { _ = resp.Body.Close() }()
	err = p.parseSSEStream(ctx, resp.Body, ch)
	if err != nil {
		select {
		case ch <- llm.StreamEvent{Type: llm.StreamError, Err: err}:
		case <-ctx.Done():
		}
	} else {
		select {
		case ch <- llm.StreamEvent{Type: llm.StreamDone}:
		case <-ctx.Done():
		}
	}
}

type toolCallBuilder struct {
	id      string
	name    string
	argsBuf strings.Builder
}

func (p *Provider) parseSSEStream(ctx context.Context, reader io.Reader, ch chan<- llm.StreamEvent) error {
	scanner := bufio.NewScanner(reader)
	toolCallsByIndex := make(map[int]*toolCallBuilder)
	toolCallOrder := make([]int, 0)
	var currentUsage llm.Usage

	flushToolCalls := func() error {
		for _, idx := range toolCallOrder {
			builder, ok := toolCallsByIndex[idx]
			if !ok {
				continue
			}
			inputStr := builder.argsBuf.String()
			if inputStr == "" {
				inputStr = "{}"
			}
			select {
			case ch <- llm.StreamEvent{
				Type: llm.StreamToolCall,
				ToolCall: &llm.ToolCall{
					ID:    builder.id,
					Name:  builder.name,
					Input: []byte(inputStr),
				},
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
			delete(toolCallsByIndex, idx)
		}
		toolCallOrder = toolCallOrder[:0]
		return nil
	}

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			return flushToolCalls()
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Error != nil && chunk.Error.Message != "" {
			return fmt.Errorf("openai error: %s", chunk.Error.Message)
		}

		if chunk.Usage != nil {
			currentUsage.InputTokens = chunk.Usage.PromptTokens
			currentUsage.OutputTokens = chunk.Usage.CompletionTokens
			currentUsage.Cost = p.calculateCost(&currentUsage)
			select {
			case ch <- llm.StreamEvent{
				Type:  llm.StreamUsage,
				Usage: &llm.Usage{InputTokens: currentUsage.InputTokens, OutputTokens: currentUsage.OutputTokens, Cost: currentUsage.Cost},
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				select {
				case ch <- llm.StreamEvent{Type: llm.StreamText, Text: choice.Delta.Content}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			for _, tc := range choice.Delta.ToolCalls {
				builder, exists := toolCallsByIndex[tc.Index]
				if !exists {
					builder = &toolCallBuilder{
						id:   tc.ID,
						name: tc.Function.Name,
					}
					toolCallsByIndex[tc.Index] = builder
					toolCallOrder = append(toolCallOrder, tc.Index)
				}
				if tc.ID != "" {
					builder.id = tc.ID
				}
				if tc.Function.Name != "" {
					builder.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					builder.argsBuf.WriteString(tc.Function.Arguments)
				}
			}

			if choice.FinishReason == "tool_calls" {
				if err := flushToolCalls(); err != nil {
					return err
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return flushToolCalls()
}
