// Package llm defines the provider-agnostic types the agent core speaks.
// Every vendor (Anthropic, OpenAI, Ollama...) is an adapter behind Provider.
package llm

import (
	"context"
	"encoding/json"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool" // result of a tool call
)

type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// Message is one entry in the conversation history.
type Message struct {
	ID         string     `json:"id,omitempty"`
	Role       Role       `json:"role"`
	Text       string     `json:"text,omitempty"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`  // assistant only
	ToolCallID string     `json:"toolCallId,omitempty"` // tool only
	IsError    bool       `json:"isError,omitempty"`    // tool only
}

// ToolSpec is what the model is told about a tool.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"` // JSON Schema for the input
}

type Usage struct {
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	Cost         float64 `json:"cost,omitempty"`
}

type Request struct {
	System    string
	Messages  []Message
	Tools     []ToolSpec
	MaxTokens int
}

type StreamEventType int

const (
	StreamText     StreamEventType = iota // incremental text
	StreamThinking                        // reasoning chunk
	StreamToolCall                        // a COMPLETE tool call (adapters assemble partial JSON)
	StreamUsage
	StreamDone
	StreamError
)

type StreamEvent struct {
	Type     StreamEventType
	Text     string
	ToolCall *ToolCall
	Usage    *Usage
	Err      error
}

// ContextLengthError indicates that a model request exceeded the context window (SPEC Amendment D).
type ContextLengthError struct {
	Provider string
	Message  string
}

func (e *ContextLengthError) Error() string {
	return e.Message
}

// ModelInfo describes a model supported by a provider (SPEC §6.3).
type ModelInfo struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	ContextWindow int     `json:"contextWindow"`
	InputPrice    float64 `json:"inputPrice,omitempty"`  // price per million tokens
	OutputPrice   float64 `json:"outputPrice,omitempty"` // price per million tokens
}

// Provider streams one model turn (SPEC §6.3).
//
// Contract: the returned channel MUST be closed when the turn ends, and the
// producer MUST stop (and close) when ctx is cancelled. Adapters send
// StreamDone on success or StreamError on failure before closing.
type Provider interface {
	Name() string
	Models() []ModelInfo
	Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}
