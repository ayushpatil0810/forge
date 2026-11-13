package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm"
)

// GenerateSessionID produces a unique session identifier.
func GenerateSessionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("s_%d_%s", time.Now().Unix(), hex.EncodeToString(b))
}

// CanonicalWorkspace returns the normalized, absolute path for a workspace.
func CanonicalWorkspace(path string) string {
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if eval, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(eval)
	}
	return filepath.Clean(abs)
}

// TruncateTitle generates a human-readable title from the first prompt (capped at 60 chars).
func TruncateTitle(prompt string) string {
	clean := strings.TrimSpace(prompt)
	// Replace newlines with spaces
	clean = strings.ReplaceAll(clean, "\r\n", " ")
	clean = strings.ReplaceAll(clean, "\n", " ")
	if len(clean) > 60 {
		return clean[:57] + "..."
	}
	if clean == "" {
		return "Untitled session"
	}
	return clean
}

// ResumeSystemNote creates the required system notice injected into resumed conversations.
func ResumeSystemNote(sessionID string) string {
	return fmt.Sprintf("[Resumed session %s. Files may have changed on disk since last session; always re-read files before editing.]", sessionID)
}

// ValidateWorkspaceBinding ensures the session belongs to the current canonical workspace root.
func ValidateWorkspaceBinding(sessionWorkspace, currentWorkspace, sessionID string) error {
	canSess := CanonicalWorkspace(sessionWorkspace)
	canCurr := CanonicalWorkspace(currentWorkspace)

	// Case-insensitive comparison on Windows
	if strings.EqualFold(canSess, canCurr) {
		return nil
	}

	return fmt.Errorf("session %s belongs to workspace %q, but current workspace is %q. Use '--root %s' to resume this session",
		sessionID, sessionWorkspace, currentWorkspace, sessionWorkspace)
}

// ReconstructedTurn represents a turn restored from persistent storage.
type ReconstructedTurn struct {
	UserPrompt    string
	AssistantText string
	ToolCalls     []ToolCallRecord
	Checkpoints   []CheckpointRecord
	UsageSegments []UsageRecord
	CreatedAt     time.Time
}

// ResumedSessionData holds the restored conversation state for viewport rebuilding.
type ResumedSessionData struct {
	Session        SessionRecord
	Messages       []MessageRecord
	ToolCallsByMsg map[string][]ToolCallRecord
	Checkpoints    []CheckpointRecord
	Usage          []UsageRecord
	LLMMessages    []llm.Message
	TotalCost      float64
	TotalTokens    int
}

// LoadResumedSession reads the session from SQLite and synthesizes interrupted tool results
// if the previous run terminated or crashed mid-turn.
func LoadResumedSession(ctx context.Context, store *Store, sessionID string) (*ResumedSessionData, error) {
	rec, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	msgs, err := store.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("loading messages: %w", err)
	}

	toolCallsMap := make(map[string][]ToolCallRecord)
	for _, m := range msgs {
		if m.Role == "assistant" {
			tcs, err := store.GetToolCalls(ctx, m.ID)
			if err == nil && len(tcs) > 0 {
				toolCallsMap[m.ID] = tcs
			}
		}
	}

	checkpoints, _ := store.GetCheckpoints(ctx, sessionID)
	usageList, _ := store.GetUsage(ctx, sessionID)

	var totalCost float64
	var totalTokens int
	for _, u := range usageList {
		totalCost += u.Cost
		totalTokens += (u.InputTokens + u.OutputTokens)
	}

	// Synthesize interrupted tool calls if dangling
	msgs = RepairInterruptedToolCalls(msgs, toolCallsMap)

	// Build provider-neutral llm.Messages for the LLM request history
	llmMsgs := make([]llm.Message, 0, len(msgs)+1)
	for _, m := range msgs {
		switch m.Role {
		case "user":
			llmMsgs = append(llmMsgs, llm.Message{Role: llm.RoleUser, Text: m.Content})
		case "assistant":
			var tcs []llm.ToolCall
			if records, ok := toolCallsMap[m.ID]; ok {
				for _, r := range records {
					tcs = append(tcs, llm.ToolCall{
						ID:    r.ID,
						Name:  r.Name,
						Input: []byte(r.Input),
					})
				}
			}
			llmMsgs = append(llmMsgs, llm.Message{
				Role:      llm.RoleAssistant,
				Text:      m.Content,
				ToolCalls: tcs,
			})
		case "tool":
			llmMsgs = append(llmMsgs, llm.Message{
				Role:       llm.RoleTool,
				Text:       m.Content,
				ToolCallID: m.ToolCallID,
			})
		}
	}

	// Inject the resume system note into conversation history
	note := ResumeSystemNote(sessionID)
	llmMsgs = append(llmMsgs, llm.Message{Role: llm.RoleUser, Text: note})

	return &ResumedSessionData{
		Session:        *rec,
		Messages:       msgs,
		ToolCallsByMsg: toolCallsMap,
		Checkpoints:    checkpoints,
		Usage:          usageList,
		LLMMessages:    llmMsgs,
		TotalCost:      totalCost,
		TotalTokens:    totalTokens,
	}, nil
}

// RepairInterruptedToolCalls checks for assistant messages with tool calls that never
// received a matching tool response (e.g. process killed mid-turn). It synthesizes
// an "interrupted" tool response so LLM providers do not reject the history.
func RepairInterruptedToolCalls(msgs []MessageRecord, toolCallsMap map[string][]ToolCallRecord) []MessageRecord {
	// Map of tool_call_id to whether a response message exists
	responded := make(map[string]bool)
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID != "" {
			responded[m.ToolCallID] = true
		}
	}

	var repaired []MessageRecord
	for i, m := range msgs {
		repaired = append(repaired, m)
		if m.Role == "assistant" {
			if tcs, ok := toolCallsMap[m.ID]; ok {
				for _, tc := range tcs {
					if !responded[tc.ID] {
						// Synthesize interrupted result
						synthetic := MessageRecord{
							ID:         fmt.Sprintf("synth_%s", tc.ID),
							SessionID:  m.SessionID,
							Role:       "tool",
							Content:    `{"error": "tool execution was interrupted by process termination or crash"}`,
							ToolCallID: tc.ID,
							CreatedAt:  m.CreatedAt.Add(time.Duration(i+1) * time.Millisecond),
						}
						repaired = append(repaired, synthetic)
						responded[tc.ID] = true
					}
				}
			}
		}
	}

	return repaired
}
