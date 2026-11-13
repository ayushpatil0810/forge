// Package anthropic implements the llm.Provider interface for Anthropic Claude models
// using raw HTTP + SSE streaming.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/retry"
)

// Source documentation:
// - Models overview & pricing: https://docs.anthropic.com/en/docs/about-claude/models
// - Model deprecations: https://docs.anthropic.com/en/docs/resources/model-deprecations
const (
	defaultEndpoint   = "https://api.anthropic.com/v1/messages"
	defaultAPIVersion = "2023-06-01"
	// Default model is claude-sonnet-5-5: active flagship offering the optimal
	// balance of reasoning capability, 1M context window, and price ($2/M in, $10/M out).
	defaultModel   = "claude-sonnet-5-5"
	maxRetries     = 3
	initialBackoff = 300 * time.Millisecond
	maxBackoff     = 8 * time.Second
)

// Active, non-deprecated Anthropic models per official documentation.
// Retired models (claude-3-5-sonnet-*, claude-3-5-haiku-*, claude-3-7-sonnet-*) have been removed.
var catalog = []llm.ModelInfo{
	{
		ID:            "claude-sonnet-5-5",
		Name:          "Claude Sonnet 5.5",
		ContextWindow: 1000000,
		InputPrice:    2.00,
		OutputPrice:   10.00,
	},
	{
		ID:            "claude-opus-5-5",
		Name:          "Claude Opus 5.5",
		ContextWindow: 1000000,
		InputPrice:    4.00,
		OutputPrice:   20.00,
	},
	{
		ID:            "claude-haiku-5-5",
		Name:          "Claude Haiku 5.5",
		ContextWindow: 1000000,
		InputPrice:    0.10,
		OutputPrice:   0.50,
	},
	{
		ID:            "claude-fable-5-2",
		Name:          "Claude Fable 5.2",
		ContextWindow: 1000000,
		InputPrice:    8.00,
		OutputPrice:   40.00,
	},
}

func init() {
	llm.RegisterProvider("anthropic", func(cfg llm.ProviderConfig) (llm.Provider, error) {
		return New(Config{
			APIKey:   cfg.APIKey,
			Model:    cfg.Model,
			Endpoint: cfg.BaseURL,
		})
	})
}

// Config holds Anthropic provider configuration options.
type Config struct {
	APIKey   string
	Model    string
	Endpoint string
	Client   *http.Client
}

// Provider implements llm.Provider for Anthropic.
type Provider struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

// New creates a new Anthropic provider.
// Fails fast with a friendly error if ANTHROPIC_API_KEY is not set.
func New(cfg Config) (*Provider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("anthropic: ANTHROPIC_API_KEY environment variable is required; please set it or run 'forge auth login anthropic'")
	}

	model := cfg.Model
	if model == "" {
		model = defaultModel
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}

	return &Provider{
		apiKey:   apiKey,
		model:    model,
		endpoint: endpoint,
		client:   client,
	}, nil
}

func (p *Provider) Name() string {
	return "anthropic"
}

func (p *Provider) Models() []llm.ModelInfo {
	return catalog
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Stream    bool               `json:"stream"`
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

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	bodyObj := anthropicRequest{
		Model:     p.model,
		MaxTokens: maxTokens,
		System:    req.System,
		Stream:    true,
	}

	for _, t := range req.Tools {
		bodyObj.Tools = append(bodyObj.Tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Schema,
		})
	}

	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleUser:
			bodyObj.Messages = append(bodyObj.Messages, anthropicMessage{
				Role:    "user",
				Content: m.Text,
			})
		case llm.RoleAssistant:
			if len(m.ToolCalls) == 0 {
				bodyObj.Messages = append(bodyObj.Messages, anthropicMessage{
					Role:    "assistant",
					Content: m.Text,
				})
			} else {
				var blocks []any
				if m.Text != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": m.Text})
				}
				for _, tc := range m.ToolCalls {
					var inputObj any
					if len(tc.Input) > 0 {
						_ = json.Unmarshal(tc.Input, &inputObj)
					}
					if inputObj == nil {
						inputObj = map[string]any{}
					}
					blocks = append(blocks, map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Name,
						"input": inputObj,
					})
				}
				bodyObj.Messages = append(bodyObj.Messages, anthropicMessage{
					Role:    "assistant",
					Content: blocks,
				})
			}
		case llm.RoleTool:
			block := map[string]any{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     m.Text,
			}
			if m.IsError {
				block["is_error"] = true
			}
			bodyObj.Messages = append(bodyObj.Messages, anthropicMessage{
				Role:    "user",
				Content: []any{block},
			})
		}
	}

	bodyBytes, err := json.Marshal(bodyObj)
	if err != nil {
		return nil, fmt.Errorf("marshaling anthropic request: %w", err)
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

	resp, err := retry.Do(ctx, retryCfg, func(opCtx context.Context) (*http.Response, bool, time.Duration, error) {
		httpReq, reqErr := http.NewRequestWithContext(opCtx, http.MethodPost, p.endpoint, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, false, 0, reqErr
		}

		httpReq.Header.Set("x-api-key", p.apiKey)
		httpReq.Header.Set("anthropic-version", defaultAPIVersion)
		httpReq.Header.Set("content-type", "application/json")
		httpReq.Header.Set("accept", "text/event-stream")

		res, doErr := p.client.Do(httpReq)
		if doErr != nil {
			if opCtx.Err() != nil {
				return nil, false, 0, opCtx.Err()
			}
			return nil, true, 0, fmt.Errorf("anthropic connection error: %w", doErr)
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
			errMsg = fmt.Sprintf("anthropic API error (%d %s): %s", res.StatusCode, errResp.Error.Type, errResp.Error.Message)
		} else {
			errMsg = fmt.Sprintf("anthropic API error (%d): %s", res.StatusCode, strings.TrimSpace(string(respBody)))
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
	// Response received; process streaming body.
	// Once streaming begins, retries are no longer attempted (A-8).
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

type blockState struct {
	blockType string
	id        string
	name      string
	jsonBuf   strings.Builder
}

func (p *Provider) parseSSEStream(ctx context.Context, reader io.Reader, ch chan<- llm.StreamEvent) error {
	scanner := bufio.NewScanner(reader)
	var currentEvent string
	var currentUsage llm.Usage
	blocks := make(map[int]*blockState)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			if err := p.dispatchSSE(ctx, currentEvent, []byte(data), ch, &currentUsage, blocks); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func (p *Provider) dispatchSSE(ctx context.Context, eventType string, data []byte, ch chan<- llm.StreamEvent, usage *llm.Usage, blocks map[int]*blockState) error {
	switch eventType {
	case "content_block_start":
		var startEv struct {
			Index        int `json:"index"`
			ContentBlock struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal(data, &startEv); err == nil {
			if startEv.ContentBlock.Type == "tool_use" {
				b := &blockState{
					blockType: "tool_use",
					id:        startEv.ContentBlock.ID,
					name:      startEv.ContentBlock.Name,
				}
				if len(startEv.ContentBlock.Input) > 0 && string(startEv.ContentBlock.Input) != "{}" {
					b.jsonBuf.Write(startEv.ContentBlock.Input)
				}
				blocks[startEv.Index] = b
			}
		}

	case "content_block_delta":
		var deltaEv struct {
			Index int `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text,omitempty"`
				Thinking    string `json:"thinking,omitempty"`
				PartialJSON string `json:"partial_json,omitempty"`
			} `json:"delta"`
		}
		if err := json.Unmarshal(data, &deltaEv); err != nil {
			return nil
		}
		if deltaEv.Delta.Type == "text_delta" && deltaEv.Delta.Text != "" {
			select {
			case ch <- llm.StreamEvent{Type: llm.StreamText, Text: deltaEv.Delta.Text}:
			case <-ctx.Done():
				return ctx.Err()
			}
		} else if deltaEv.Delta.Type == "thinking_delta" && deltaEv.Delta.Thinking != "" {
			select {
			case ch <- llm.StreamEvent{Type: llm.StreamThinking, Text: deltaEv.Delta.Thinking}:
			case <-ctx.Done():
				return ctx.Err()
			}
		} else if deltaEv.Delta.Type == "input_json_delta" && deltaEv.Delta.PartialJSON != "" {
			if b, ok := blocks[deltaEv.Index]; ok {
				b.jsonBuf.WriteString(deltaEv.Delta.PartialJSON)
			}
		}

	case "content_block_stop":
		var stopEv struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal(data, &stopEv); err == nil {
			if b, ok := blocks[stopEv.Index]; ok && b.blockType == "tool_use" {
				inputStr := b.jsonBuf.String()
				if inputStr == "" {
					inputStr = "{}"
				}
				select {
				case ch <- llm.StreamEvent{
					Type: llm.StreamToolCall,
					ToolCall: &llm.ToolCall{
						ID:    b.id,
						Name:  b.name,
						Input: []byte(inputStr),
					},
				}:
				case <-ctx.Done():
					return ctx.Err()
				}
				delete(blocks, stopEv.Index)
			}
		}

	case "message_start":
		var startEv struct {
			Message struct {
				Usage struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(data, &startEv); err == nil && startEv.Message.Usage.InputTokens > 0 {
			usage.InputTokens = startEv.Message.Usage.InputTokens
			usage.Cost = p.calculateCost(usage)
			select {
			case ch <- llm.StreamEvent{Type: llm.StreamUsage, Usage: &llm.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, Cost: usage.Cost}}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

	case "message_delta":
		var deltaEv struct {
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &deltaEv); err == nil && deltaEv.Usage.OutputTokens > 0 {
			usage.OutputTokens = deltaEv.Usage.OutputTokens
			usage.Cost = p.calculateCost(usage)
			select {
			case ch <- llm.StreamEvent{Type: llm.StreamUsage, Usage: &llm.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, Cost: usage.Cost}}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

	case "error":
		var errEv struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &errEv); err == nil && errEv.Error.Message != "" {
			return fmt.Errorf("anthropic error: %s", errEv.Error.Message)
		}
	}
	return nil
}
