package mock

import (
	"fmt"

	"github.com/ayushpatil0810/forge/internal/llm"
)

// Validator enforces provider message format rules for Anthropic, OpenAI, and Gemini.
// Reference documentation:
// - Anthropic: https://docs.anthropic.com/en/docs/build-with-claude/tool-use
// - OpenAI: https://platform.openai.com/docs/guides/function-calling
// - Gemini: https://ai.google.dev/gemini-api/docs/function-calling
type Validator struct{}

// NewValidator creates a new strict history validator.
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateAnthropic enforces Anthropic Messages API constraints:
// 1. First message must have Role "user".
// 2. Roles must strictly alternate between user and assistant (tool results are mapped to user turn).
// 3. Every tool call in an assistant message must have a corresponding tool result.
// 4. No orphan tool results (tool results without matching prior tool call).
// Doc: https://docs.anthropic.com/en/docs/build-with-claude/tool-use
func (v *Validator) ValidateAnthropic(msgs []llm.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	if msgs[0].Role != llm.RoleUser {
		return fmt.Errorf("anthropic: first message must have role 'user', got %q", msgs[0].Role)
	}

	var pendingCalls = make(map[string]bool)
	var lastRole llm.Role

	for i, m := range msgs {
		switch m.Role {
		case llm.RoleUser:
			if len(pendingCalls) > 0 {
				return fmt.Errorf("anthropic: message %d is 'user' but tool calls %v are still pending", i, pendingCalls)
			}
			if lastRole == llm.RoleUser {
				return fmt.Errorf("anthropic: consecutive 'user' messages at index %d violate role alternation", i)
			}
			lastRole = llm.RoleUser

		case llm.RoleAssistant:
			if len(pendingCalls) > 0 {
				return fmt.Errorf("anthropic: message %d is 'assistant' but tool calls %v are still pending", i, pendingCalls)
			}
			if lastRole == llm.RoleAssistant {
				return fmt.Errorf("anthropic: consecutive 'assistant' messages at index %d violate role alternation", i)
			}
			for _, tc := range m.ToolCalls {
				if tc.ID == "" {
					return fmt.Errorf("anthropic: tool call at message %d has empty ID", i)
				}
				pendingCalls[tc.ID] = true
			}
			lastRole = llm.RoleAssistant

		case llm.RoleTool:
			if lastRole != llm.RoleAssistant && lastRole != llm.RoleTool {
				return fmt.Errorf("anthropic: tool message at index %d must follow an assistant message or preceding tool result", i)
			}
			if !pendingCalls[m.ToolCallID] {
				return fmt.Errorf("anthropic: orphan tool result for ID %q at index %d", m.ToolCallID, i)
			}
			delete(pendingCalls, m.ToolCallID)
			lastRole = llm.RoleTool

		default:
			return fmt.Errorf("anthropic: unknown role %q at index %d", m.Role, i)
		}
	}

	if len(pendingCalls) > 0 {
		return fmt.Errorf("anthropic: conversation ended with pending tool calls: %v", pendingCalls)
	}
	return nil
}

// ValidateOpenAI enforces OpenAI Chat Completions API constraints:
// 1. Tool messages must have matching tool_call_id from the preceding assistant message.
// 2. All tool calls in an assistant message must be answered before a subsequent assistant or user message.
// 3. No orphan tool responses.
// Doc: https://platform.openai.com/docs/guides/function-calling
func (v *Validator) ValidateOpenAI(msgs []llm.Message) error {
	var pendingCalls = make(map[string]bool)

	for i, m := range msgs {
		switch m.Role {
		case llm.RoleUser:
			if len(pendingCalls) > 0 {
				return fmt.Errorf("openai: message %d is 'user' but tool calls %v are still pending", i, pendingCalls)
			}
		case llm.RoleAssistant:
			if len(pendingCalls) > 0 {
				return fmt.Errorf("openai: message %d is 'assistant' but tool calls %v are still pending", i, pendingCalls)
			}
			for _, tc := range m.ToolCalls {
				if tc.ID == "" {
					return fmt.Errorf("openai: tool call at message %d has empty ID", i)
				}
				pendingCalls[tc.ID] = true
			}
		case llm.RoleTool:
			if !pendingCalls[m.ToolCallID] {
				return fmt.Errorf("openai: orphan tool result for ID %q at index %d", m.ToolCallID, i)
			}
			delete(pendingCalls, m.ToolCallID)
		default:
			return fmt.Errorf("openai: unknown role %q at index %d", m.Role, i)
		}
	}

	if len(pendingCalls) > 0 {
		return fmt.Errorf("openai: conversation ended with pending tool calls: %v", pendingCalls)
	}
	return nil
}

// ValidateGemini enforces Gemini generateContent API constraints:
// 1. First message must be 'user'.
// 2. Contents roles alternate user -> model -> user -> model.
// 3. Function responses must pair with prior function calls.
// Doc: https://ai.google.dev/gemini-api/docs/function-calling
func (v *Validator) ValidateGemini(msgs []llm.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	if msgs[0].Role != llm.RoleUser {
		return fmt.Errorf("gemini: first message must have role 'user', got %q", msgs[0].Role)
	}

	var pendingCalls = make(map[string]bool)
	var lastRole llm.Role

	for i, m := range msgs {
		switch m.Role {
		case llm.RoleUser:
			if len(pendingCalls) > 0 {
				return fmt.Errorf("gemini: message %d is 'user' but tool calls %v are still pending", i, pendingCalls)
			}
			if lastRole == llm.RoleUser {
				return fmt.Errorf("gemini: consecutive 'user' messages at index %d violate role alternation", i)
			}
			lastRole = llm.RoleUser

		case llm.RoleAssistant:
			if len(pendingCalls) > 0 {
				return fmt.Errorf("gemini: message %d is 'assistant' but tool calls %v are still pending", i, pendingCalls)
			}
			if lastRole == llm.RoleAssistant {
				return fmt.Errorf("gemini: consecutive 'assistant' messages at index %d violate role alternation", i)
			}
			for _, tc := range m.ToolCalls {
				pendingCalls[tc.ID] = true
			}
			lastRole = llm.RoleAssistant

		case llm.RoleTool:
			if !pendingCalls[m.ToolCallID] {
				return fmt.Errorf("gemini: orphan tool result for ID %q at index %d", m.ToolCallID, i)
			}
			delete(pendingCalls, m.ToolCallID)
			lastRole = llm.RoleTool

		default:
			return fmt.Errorf("gemini: unknown role %q at index %d", m.Role, i)
		}
	}

	if len(pendingCalls) > 0 {
		return fmt.Errorf("gemini: conversation ended with pending tool calls: %v", pendingCalls)
	}
	return nil
}

// ValidateAll validates the history against Anthropic, OpenAI, and Gemini constraints.
func (v *Validator) ValidateAll(msgs []llm.Message) error {
	if err := v.ValidateAnthropic(msgs); err != nil {
		return err
	}
	if err := v.ValidateOpenAI(msgs); err != nil {
		return err
	}
	if err := v.ValidateGemini(msgs); err != nil {
		return err
	}
	return nil
}
