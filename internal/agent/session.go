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
	ID               string
	History          []llm.Message
	Provider         llm.Provider
	Tools            *tools.Registry
	Policy           permission.Policy
	PolicySet        *policy.PolicySet
	AuditLogger      *audit.Logger
	Checkpointer     Checkpointer
	System           string
	MaxTokens        int
	MaxTurns         int // hard cap on model calls per Send; guards runaway tool loops (A-5)
	MaxRepeatedCalls int // threshold for repeated identical tool calls (A-5)
	IsHeadless       bool
	AutoApprove      bool
}
