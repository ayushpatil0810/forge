// Package agent is the UI-agnostic engine. The TUI, the Wails desktop app and
// the headless CLI all drive it through the Session interface and nothing else.
package agent

import (
	"context"
	"errors"
	"time"

	"github.com/ayushpatil0810/forge/internal/audit"
	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/policy"
	"github.com/ayushpatil0810/forge/internal/tools"
)

type Decision string

const (
	DecisionAllow            Decision = "allow"              // 'y': allow once
	DecisionAllowSession     Decision = "allow_session"      // 'a': allow for this tool in this session
	DecisionAlwaysAllow      Decision = "always_allow"       // 'A': always allow, saved to user policy
	DecisionAllowAlways      Decision = "always_allow"       // alias for backward compatibility
	DecisionDeny             Decision = "deny"               // 'n' or 'd': deny once
	DecisionDenyWithFeedback Decision = "deny_with_feedback" // 'f': deny with user feedback
)

var (
	ErrBusy            = errors.New("agent: a turn is already running")
	ErrClosed          = errors.New("agent: session is closed")
	ErrUnknownApproval = errors.New("agent: no pending approval with that id")
)

// Usage aliases UsageUpdatedPayload for convenient frontend access.
type Usage = UsageUpdatedPayload

// ContextUsage aliases ContextUsagePayload for convenient frontend access.
type ContextUsage = ContextUsagePayload

// CompactionRecord represents a recorded context compaction event.
type CompactionRecord struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"session_id"`
	Tier           int       `json:"tier"`
	Summary        string    `json:"summary"`
	FirstMessageID string    `json:"first_message_id"`
	LastMessageID  string    `json:"last_message_id"`
	TokensBefore   int       `json:"tokens_before"`
	TokensAfter    int       `json:"tokens_after"`
	CreatedAt      time.Time `json:"created_at"`
}

// CompactionStore persists compaction records to storage.
type CompactionStore interface {
	SaveCompaction(ctx context.Context, c CompactionRecord) error
}

// Session is the whole contract between the engine and any frontend (SPEC §6.3).
// A future remote (daemon) implementation can satisfy it unchanged.
type Session interface {
	// ID returns the unique session identifier.
	ID() string
	// Send starts a turn. It takes a context for cancellation and returns immediately; progress arrives on Events.
	Send(ctx context.Context, prompt string) error
	// Events delivers progress. Consumers MUST keep draining it. Each Send
	// ends with exactly one terminal event (Done or Error).
	Events() <-chan Event
	// Respond answers an ApprovalRequested event, optionally providing feedback on denial.
	Respond(approvalID string, d Decision, feedback ...string) error
	// Cancel aborts the running turn, if any.
	Cancel()
	// Usage returns the cumulative usage for the session.
	Usage() Usage
	// ContextUsage returns the current context window usage details.
	ContextUsage() ContextUsage
	// Compact runs manual context compaction with optional focus text.
	Compact(ctx context.Context, focusText ...string) error
	// SetBudget dynamically updates the run token or cost budget (e.g. via /budget command).
	SetBudget(maxCost float64, maxTokens int)
	// History returns a copy of the active in-memory history (uncompacted transcript).
	History() []llm.Message
	// ModelView returns a copy of the compacted history sent to the model.
	ModelView() []llm.Message
	// Mode returns the active permission mode.
	Mode() permission.Mode
	// SetMode dynamically updates the permission mode (SPEC §3.3).
	SetMode(m permission.Mode)
	// Checkpointer returns the active checkpoint manager, if configured.
	Checkpointer() Checkpointer
	// PolicySet returns the active policy rules, if configured.
	PolicySet() *policy.PolicySet
	// SetProvider dynamically updates the active LLM provider.
	SetProvider(p llm.Provider)
	// Close cancels, waits for the turn to stop, then closes Events.
	Close()
}

// CheckpointInfo contains metadata about a recorded or restored snapshot.
type CheckpointInfo struct {
	ID           string
	Message      string
	Timestamp    time.Time
	SkippedFiles []string
}

// Checkpointer abstracts shadow repository snapshotting and undo operations.
type Checkpointer interface {
	Create(ctx context.Context, message string) (CheckpointInfo, error)
	Restore(ctx context.Context, id string) (CheckpointInfo, error)
	List(ctx context.Context, limit int) ([]CheckpointInfo, error)
}

type Config struct {
	ID                 string
	History            []llm.Message
	Provider           llm.Provider
	Tools              *tools.Registry
	Policy             permission.Policy
	PolicySet          *policy.PolicySet
	AuditLogger        *audit.Logger
	Checkpointer       Checkpointer
	System             string
	MaxTokens          int
	MaxTurns           int // hard cap on model calls per Send; guards runaway tool loops (A-5)
	MaxRepeatedCalls   int // threshold for repeated identical tool calls (A-5)
	IsHeadless         bool
	AutoApprove        bool
	ContextWindow      int
	AutoCompact        bool
	NoAutoCompact      bool
	Tier1Threshold     float64
	Tier2Threshold     float64
	EmergencyThreshold float64
	KeepRecentTurns    int
	SummaryMaxTokens   int
	MaxCostBudget      float64
	MaxTokensBudget    int
	SessionStore       CompactionStore
	InitialTokens      int
	InitialCost        float64
	Compactions        []CompactionRecord
}
