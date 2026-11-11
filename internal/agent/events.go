package agent

import "github.com/ayushpatil0810/forge/internal/llm"

type EventType string

const (
	EventTextDelta         EventType = "text_delta"
	EventToolStarted       EventType = "tool_started"
	EventApprovalRequested EventType = "approval_requested"
	EventToolFinished      EventType = "tool_finished"
	EventUsage             EventType = "usage"
	EventDone              EventType = "done"      // terminal
	EventError             EventType = "error"     // terminal
	EventCancelled         EventType = "cancelled" // terminal
)

type ToolStatus string

const (
	ToolPending   ToolStatus = "pending"
	ToolOK        ToolStatus = "ok"
	ToolError     ToolStatus = "error"
	ToolDenied    ToolStatus = "denied"
	ToolCancelled ToolStatus = "cancelled"
)

// ToolEvent describes a tool call's lifecycle. Strings only, so Wails
// generates clean TypeScript types for the React frontend.
type ToolEvent struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Input      string     `json:"input"`
	Risk       string     `json:"risk"`
	Status     ToolStatus `json:"status"`
	Output     string     `json:"output,omitempty"`
	DurationMs int64      `json:"durationMs,omitempty"`
}

type ApprovalRequest struct {
	ID   string    `json:"id"` // equals the tool call ID
	Tool ToolEvent `json:"tool"`
	// Diff string — populated once write/edit tools exist.
}

// Event is the single, flat, JSON-friendly envelope both UIs consume.
// Switch on Type and read the matching field.
type Event struct {
	Seq      int64            `json:"seq"`
	Type     EventType        `json:"type"`
	Text     string           `json:"text,omitempty"`
	Tool     *ToolEvent       `json:"tool,omitempty"`
	Approval *ApprovalRequest `json:"approval,omitempty"`
	Usage    *llm.Usage       `json:"usage,omitempty"`
	Error    string           `json:"error,omitempty"`
}

func (e Event) Terminal() bool {
	return e.Type == EventDone || e.Type == EventError || e.Type == EventCancelled
}
