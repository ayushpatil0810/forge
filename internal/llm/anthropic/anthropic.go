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
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm"
)

const (
	defaultEndpoint   = "https://api.anthropic.com/v1/messages"
	defaultAPIVersion = "2023-06-01"
	defaultModel      = "claude-3-5-sonnet-20241022"
	maxRetries        = 3
	initialBackoff    = 300 * time.Millisecond
	maxBackoff        = 8 * time.Second
)

var catalog = []llm.ModelInfo{
	{
		ID:            "claude-3-7-sonnet-20250219",
		Name:          "Claude 3.7 Sonnet",
		ContextWindow: 200000,
		InputPrice:    3.00,
		OutputPrice:   15.00,
	},
	{
		ID:            "claude-3-5-sonnet-20241022",
		Name:          "Claude 3.5 Sonnet",
		ContextWindow: 200000,
		InputPrice:    3.00,
		OutputPrice:   15.00,
	},
	{
		ID:            "claude-3-5-haiku-20241022",
		Name:          "Claude 3.5 Haiku",
		ContextWindow: 200000,
		InputPrice:    0.80,
		OutputPrice:   4.00,
	},
	{
		ID:            "claude-opus-5-5",
		Name:          "Claude Opus 5.5",
		ContextWindow: 1000000,
		InputPrice:    4.00,
		OutputPrice:   20.00,
	},
}

// Config configures the Anthropic provider.
type Config struct {
	APIKey   string
	Model    string
	Endpoint string
	Client   *http.Client
}

// Provider implements llm.Provider.
type Provider struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

// New creates an Anthropic provider. It reads ANTHROPIC_API_KEY from environment if not provided.
func New(cfg Config) (*Provider, error) {
	key := cfg.APIKey
	if key == "" {
		key = os.Getenv("ANTHROPIC_API_KEY")
	}
	if key == "" {
		return nil, errors.New("ANTHROPIC_API_KEY environment variable is not set; please set it or run with --demo")
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
		client = &http.Client{Timeout: 0} // No timeout on client; turn context controls duration
	}

	return &Provider{
		apiKey:   key,
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

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream"`
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

	for _, m := range req.Messages {
		role := string(m.Role)
		if m.Role == llm.RoleTool {
			role = "user"
		}
		bodyObj.Messages = append(bodyObj.Messages, anthropicMessage{
			Role:    role,
			Content: m.Text,
		})
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

	backoff := initialBackoff
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, retryable, err := p.sendRequest(ctx, bodyBytes)
		if err != nil {
			if !retryable || attempt == maxRetries {
				select {
				case ch <- llm.StreamEvent{Type: llm.StreamError, Err: err}:
				case <-ctx.Done():
				}
				return
			}

			// Calculate backoff with jitter
			sleepDuration := backoff + time.Duration(rand.Int63n(int64(backoff/2)))
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(sleepDuration):
				continue
			}
		}

		// Response received; process streaming body.
		// Once streaming begins, retries are no longer attempted (A-8).
		err = parseSSEStream(ctx, resp.Body, ch)
		_ = resp.Body.Close()
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
		return
	}
}

func (p *Provider) sendRequest(ctx context.Context, bodyBytes []byte) (*http.Response, bool, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, false, err
	}

	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", defaultAPIVersion)
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		// Network errors are retryable
		return nil, true, fmt.Errorf("anthropic connection error: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		return resp, false, nil
	}

	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	// Determine retryability per requirement A-8:
	// Retry on 429, 5xx, and 529 (overloaded). Do NOT retry 400, 401, etc.
	status := resp.StatusCode
	isRetryable := status == 429 || status == 529 || (status >= 500 && status <= 599)

	var errMsg string
	var errResp struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
		errMsg = fmt.Sprintf("anthropic API error (%d %s): %s", status, errResp.Error.Type, errResp.Error.Message)
	} else {
		errMsg = fmt.Sprintf("anthropic API error (%d): %s", status, strings.TrimSpace(string(respBody)))
	}

	// If Retry-After header is provided, inspect it
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" && isRetryable {
		if seconds, parseErr := strconv.Atoi(strings.TrimSpace(retryAfter)); parseErr == nil && seconds > 0 {
			// Cap sleep at maxBackoff
			sleepDur := min(time.Duration(seconds)*time.Second, maxBackoff)
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			case <-time.After(sleepDur):
			}
		}
	}

	return nil, isRetryable, errors.New(errMsg)
}

func parseSSEStream(ctx context.Context, reader io.Reader, ch chan<- llm.StreamEvent) error {
	scanner := bufio.NewScanner(reader)
	var currentEvent string
	var currentUsage llm.Usage

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Text()
		if line == "" {
			currentEvent = ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}

			if err := dispatchSSE(ctx, currentEvent, []byte(data), ch, &currentUsage); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func dispatchSSE(ctx context.Context, eventType string, data []byte, ch chan<- llm.StreamEvent, usage *llm.Usage) error {
	switch eventType {
	case "content_block_delta":
		var deltaEv struct {
			Delta struct {
				Type     string `json:"type"`
				Text     string `json:"text,omitempty"`
				Thinking string `json:"thinking,omitempty"`
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
			select {
			case ch <- llm.StreamEvent{Type: llm.StreamUsage, Usage: &llm.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}}:
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
			select {
			case ch <- llm.StreamEvent{Type: llm.StreamUsage, Usage: &llm.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}}:
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
