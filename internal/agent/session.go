// Package agent is the UI-agnostic engine. The TUI, the Wails desktop app and
// the headless CLI all drive it through the Session interface and nothing else.
package agent

import (
	"context"
	"errors"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tools"
)

type Decision string

const (
	DecisionAllow       Decision = "allow"
	DecisionAllowAlways Decision = "allow_always" // for this tool, this session
	DecisionDeny        Decision = "deny"
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
	// Respond answers an ApprovalRequested event.
	Respond(approvalID string, d Decision) error
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
	Checkpointer     Checkpointer
	System           string
	MaxTokens        int
	MaxTurns         int // hard cap on model calls per Send; guards runaway tool loops (A-5)
	MaxRepeatedCalls int // threshold for repeated identical tool calls (A-5)
}
