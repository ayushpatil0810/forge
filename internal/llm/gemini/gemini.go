// Package gemini implements the llm.Provider interface for Google Gemini models
// using the Gemini Developer API with SSE streaming.
package gemini

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
	"sync/atomic"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/retry"
)

// Source documentation:
// - Models & pricing: https://ai.google.dev/pricing
const (
	defaultEndpoint = "https://generativelanguage.googleapis.com"
	// Default model is gemini-2.5-pro: state-of-the-art multimodal reasoning model
	// with ~1M context window ($1.25/M in, $10.00/M out).
	defaultModel   = "gemini-2.5-pro"
	maxRetries     = 3
	initialBackoff = 300 * time.Millisecond
	maxBackoff     = 8 * time.Second
)

// Active Gemini Developer API models per official documentation.
// Retired models (gemini-2.0-flash) have been removed.
var catalog = []llm.ModelInfo{
	{
		ID:            "gemini-2.5-pro",
		Name:          "Gemini 2.5 Pro",
		ContextWindow: 1048576,
		InputPrice:    1.25,
		OutputPrice:   10.00,
	},
	{
		ID:            "gemini-2.5-flash",
		Name:          "Gemini 2.5 Flash",
		ContextWindow: 1048576,
		InputPrice:    0.30,
		OutputPrice:   2.50,
	},
}

func init() {
	llm.RegisterProvider("gemini", func(cfg llm.ProviderConfig) (llm.Provider, error) {
		return New(Config{
			APIKey:   cfg.APIKey,
			Model:    cfg.Model,
			Endpoint: cfg.BaseURL,
		})
	})
}

// Config holds Gemini provider configuration options.
type Config struct {
	APIKey   string
	Model    string
	Endpoint string
	Client   *http.Client
}

// Provider implements llm.Provider for Google Gemini.
type Provider struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
	callSeq  atomic.Int64
}

// New creates a new Gemini provider.
// Fails fast with a friendly error if neither GEMINI_API_KEY nor GOOGLE_API_KEY is set.
func New(cfg Config) (*Provider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("gemini: GEMINI_API_KEY (or GOOGLE_API_KEY) environment variable is required; please set it or run 'forge auth login gemini'")
	}

	model := cfg.Model
	if model == "" {
		model = defaultModel
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	endpoint = strings.TrimRight(endpoint, "/")

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
	return "gemini"
}

func (p *Provider) Models() []llm.ModelInfo {
	return catalog
}

type geminiPart struct {
	Text             string              `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResp `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type geminiFunctionResp struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiFunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations"`
}

type geminiInstruction struct {
	Parts []geminiPart `json:"parts"`
}

type geminiGenConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiRequest struct {
	SystemInstruction *geminiInstruction `json:"systemInstruction,omitempty"`
	Contents          []geminiContent    `json:"contents"`
	Tools             []geminiTool       `json:"tools,omitempty"`
	GenerationConfig  *geminiGenConfig   `json:"generationConfig,omitempty"`
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

	bodyObj := geminiRequest{}

	if req.MaxTokens > 0 {
		bodyObj.GenerationConfig = &geminiGenConfig{MaxOutputTokens: req.MaxTokens}
	}

	if req.System != "" {
		bodyObj.SystemInstruction = &geminiInstruction{
			Parts: []geminiPart{{Text: req.System}},
		}
	}

	for _, m := range req.Messages {
		if m.Role == llm.RoleTool {
			toolName := m.ToolCallID
			if strings.HasPrefix(toolName, "call_gemini_") {
				subparts := strings.Split(toolName, "_")
				if len(subparts) >= 3 {
					toolName = subparts[2]
				}
			}
			respMap := map[string]any{"output": m.Text}
			if m.IsError {
				respMap = map[string]any{"error": m.Text}
			}
			bodyObj.Contents = append(bodyObj.Contents, geminiContent{
				Role: "function",
				Parts: []geminiPart{
					{
						FunctionResponse: &geminiFunctionResp{
							Name:     toolName,
							Response: map[string]any{"response": respMap},
						},
					},
				},
			})
			continue
		}

		role := "user"
		if m.Role == llm.RoleAssistant {
			role = "model"
		}

		var parts []geminiPart
		if m.Text != "" {
			parts = append(parts, geminiPart{Text: m.Text})
		}
		for _, tc := range m.ToolCalls {
			var args map[string]any
			_ = json.Unmarshal(tc.Input, &args)
			parts = append(parts, geminiPart{
				FunctionCall: &geminiFunctionCall{
					Name: tc.Name,
					Args: args,
				},
			})
		}

		if len(parts) > 0 {
			bodyObj.Contents = append(bodyObj.Contents, geminiContent{
				Role:  role,
				Parts: parts,
			})
		}
	}

	if len(req.Tools) > 0 {
		var decls []geminiFunctionDecl
		for _, t := range req.Tools {
			decls = append(decls, geminiFunctionDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  SanitizeSchema(t.Schema),
			})
		}
		bodyObj.Tools = []geminiTool{{FunctionDeclarations: decls}}
	}

	bodyBytes, err := json.Marshal(bodyObj)
	if err != nil {
		return nil, fmt.Errorf("marshaling gemini request: %w", err)
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

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", p.endpoint, p.model)

	resp, err := retry.Do(ctx, retryCfg, func(opCtx context.Context) (*http.Response, bool, time.Duration, error) {
		httpReq, reqErr := http.NewRequestWithContext(opCtx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, false, 0, reqErr
		}

		httpReq.Header.Set("x-goog-api-key", p.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		res, doErr := p.client.Do(httpReq)
		if doErr != nil {
			if opCtx.Err() != nil {
				return nil, false, 0, opCtx.Err()
			}
			return nil, true, 0, fmt.Errorf("gemini connection error: %w", doErr)
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
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			errMsg = fmt.Sprintf("gemini API error (%d %s): %s", res.StatusCode, errResp.Error.Status, errResp.Error.Message)
		} else {
			errMsg = fmt.Sprintf("gemini API error (%d): %s", res.StatusCode, strings.TrimSpace(string(respBody)))
		}

		if res.StatusCode == http.StatusBadRequest {
			lowerMsg := strings.ToLower(errResp.Error.Message)
			// Gemini context length error: INVALID_ARGUMENT and exceeds maximum tokens (Amendment D).
			// Docs: https://ai.google.dev/gemini-api/docs/troubleshooting
			if errResp.Error.Status == "INVALID_ARGUMENT" &&
				(strings.Contains(lowerMsg, "exceeds the maximum number of tokens allowed") ||
					(strings.Contains(lowerMsg, "token count") && strings.Contains(lowerMsg, "exceeds"))) {
				return nil, false, 0, &llm.ContextLengthError{
					Provider: "gemini",
					Message:  errMsg,
				}
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

func (p *Provider) parseSSEStream(ctx context.Context, reader io.Reader, ch chan<- llm.StreamEvent) error {
	scanner := bufio.NewScanner(reader)
	var currentUsage llm.Usage

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

		var chunk struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text         string              `json:"text"`
						FunctionCall *geminiFunctionCall `json:"functionCall"`
					} `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
			} `json:"candidates"`
			UsageMetadata *struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
			} `json:"usageMetadata"`
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Error != nil && chunk.Error.Message != "" {
			return fmt.Errorf("gemini error: %s", chunk.Error.Message)
		}

		if chunk.UsageMetadata != nil {
			currentUsage.InputTokens = chunk.UsageMetadata.PromptTokenCount
			currentUsage.OutputTokens = chunk.UsageMetadata.CandidatesTokenCount
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

		for _, cand := range chunk.Candidates {
			for _, part := range cand.Content.Parts {
				if part.Text != "" {
					select {
					case ch <- llm.StreamEvent{Type: llm.StreamText, Text: part.Text}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}

				if part.FunctionCall != nil {
					callID := fmt.Sprintf("call_gemini_%s_%d", part.FunctionCall.Name, p.callSeq.Add(1))
					argsBytes, _ := json.Marshal(part.FunctionCall.Args)
					if len(argsBytes) == 0 {
						argsBytes = []byte("{}")
					}
					select {
					case ch <- llm.StreamEvent{
						Type: llm.StreamToolCall,
						ToolCall: &llm.ToolCall{
							ID:    callID,
							Name:  part.FunctionCall.Name,
							Input: argsBytes,
						},
					}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
		}
	}

	return scanner.Err()
}
