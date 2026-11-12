package agent

type EventType string

const (
	EventTokenDelta        EventType = "TokenDelta"
	EventThinkingDelta     EventType = "ThinkingDelta"
	EventToolStarted       EventType = "ToolStarted"
	EventToolOutputDelta   EventType = "ToolOutputDelta"
	EventToolFinished      EventType = "ToolFinished"
	EventApprovalRequested EventType = "ApprovalRequested"
	EventCheckpointCreated EventType = "CheckpointCreated"
	EventUsageUpdated      EventType = "UsageUpdated"
	EventError             EventType = "Error"
	EventDone              EventType = "Done"
)

type ToolStatus string

const (
	ToolPending   ToolStatus = "pending"
	ToolRunning   ToolStatus = "running"
	ToolOK        ToolStatus = "ok"
	ToolError     ToolStatus = "error"
	ToolDenied    ToolStatus = "denied"
	ToolCancelled ToolStatus = "cancelled"
)

// Typed payloads for events defined in SPEC §6.4.
// All fields are JSON-serializable so Wails can generate clean TypeScript types.

type TokenDeltaPayload struct {
	Text string `json:"text"`
}

type ThinkingDeltaPayload struct {
	Thinking string `json:"thinking"`
}

type ToolStartedPayload struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

type ToolOutputDeltaPayload struct {
	ID    string `json:"id"`
	Chunk string `json:"chunk"`
}

type ToolFinishedPayload struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Result     string     `json:"result"`
	DurationMs int64      `json:"durationMs"`
	Error      string     `json:"error,omitempty"`
	Status     ToolStatus `json:"status"`
}

type ApprovalRequestedPayload struct {
	ID             string `json:"id"` // equals the tool call ID
	Tool           string `json:"tool"`
	Input          string `json:"input"`
	Description    string `json:"description,omitempty"`
	Diff           string `json:"diff,omitempty"`
	Command        string `json:"command,omitempty"`
	Cwd            string `json:"cwd,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	IsDangerous    bool   `json:"is_dangerous,omitempty"`
}

type CheckpointCreatedPayload struct {
	ID    string   `json:"id"`
	Files []string `json:"files"`
}

type UsageUpdatedPayload struct {
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	Cost         float64 `json:"cost"`
}

type ErrorPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type DonePayload struct {
	Reason string `json:"reason"`
}

// Event is the single, flat, JSON-friendly envelope frontends consume (SPEC §6.4).
type Event struct {
	Seq       int64                     `json:"seq"`
	Type      EventType                 `json:"type"`
	Token     *TokenDeltaPayload        `json:"token,omitempty"`
	Thinking  *ThinkingDeltaPayload     `json:"thinking,omitempty"`
	ToolStart *ToolStartedPayload       `json:"toolStart,omitempty"`
	ToolOut   *ToolOutputDeltaPayload   `json:"toolOut,omitempty"`
	ToolDone  *ToolFinishedPayload      `json:"toolDone,omitempty"`
	Approval  *ApprovalRequestedPayload `json:"approval,omitempty"`
	Checkpt   *CheckpointCreatedPayload `json:"checkpt,omitempty"`
	Usage     *UsageUpdatedPayload      `json:"usage,omitempty"`
	Err       *ErrorPayload             `json:"err,omitempty"`
	Done      *DonePayload              `json:"done,omitempty"`
}

func (e Event) Terminal() bool {
	return e.Type == EventDone || e.Type == EventError
}
