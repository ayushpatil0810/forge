package mock_test

import (
	"encoding/json"
	"testing"

	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
)

func TestStrictValidator_ValidHistories(t *testing.T) {
	v := mock.NewValidator()

	// 1. Simple single turn
	h1 := []llm.Message{
		{Role: llm.RoleUser, Text: "Hello"},
		{Role: llm.RoleAssistant, Text: "Hi there!"},
	}
	if err := v.ValidateAll(h1); err != nil {
		t.Fatalf("expected valid history, got: %v", err)
	}

	// 2. Turn with parallel tool calls
	h2 := []llm.Message{
		{Role: llm.RoleUser, Text: "Check files"},
		{Role: llm.RoleAssistant, Text: "Checking...", ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "read_file", Input: json.RawMessage(`{"path":"a.txt"}`)},
			{ID: "call_2", Name: "read_file", Input: json.RawMessage(`{"path":"b.txt"}`)},
		}},
		{Role: llm.RoleTool, ToolCallID: "call_1", Text: "content of a"},
		{Role: llm.RoleTool, ToolCallID: "call_2", Text: "content of b"},
		{Role: llm.RoleAssistant, Text: "Done reading files"},
	}
	if err := v.ValidateAll(h2); err != nil {
		t.Fatalf("expected valid history with parallel tool calls, got: %v", err)
	}
}

func TestStrictValidator_InvalidHistories(t *testing.T) {
	v := mock.NewValidator()

	// 1. First message not user
	h1 := []llm.Message{
		{Role: llm.RoleAssistant, Text: "I started first"},
	}
	if err := v.ValidateAnthropic(h1); err == nil {
		t.Errorf("expected Anthropic to reject assistant as first message")
	}
	if err := v.ValidateGemini(h1); err == nil {
		t.Errorf("expected Gemini to reject assistant as first message")
	}

	// 2. Orphan tool result
	h2 := []llm.Message{
		{Role: llm.RoleUser, Text: "Hello"},
		{Role: llm.RoleTool, ToolCallID: "orphan_call", Text: "orphan result"},
	}
	if err := v.ValidateAll(h2); err == nil {
		t.Errorf("expected validator to reject orphan tool result")
	}

	// 3. Unresolved tool calls (tool call without matching tool result)
	h3 := []llm.Message{
		{Role: llm.RoleUser, Text: "Hello"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "bash", Input: json.RawMessage(`{"command":"ls"}`)},
		}},
		{Role: llm.RoleUser, Text: "Next message without tool result"},
	}
	if err := v.ValidateAll(h3); err == nil {
		t.Errorf("expected validator to reject unresolved tool call")
	}

	// 4. Consecutive user messages (rejected by Anthropic / Gemini alternation)
	h4 := []llm.Message{
		{Role: llm.RoleUser, Text: "Message 1"},
		{Role: llm.RoleUser, Text: "Message 2"},
	}
	if err := v.ValidateAnthropic(h4); err == nil {
		t.Errorf("expected Anthropic to reject consecutive user messages")
	}
}
