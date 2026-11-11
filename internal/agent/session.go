// Package agent is the UI-agnostic engine. The TUI, the Wails desktop app and
// the headless CLI all drive it through the Session interface and nothing else.
package agent

import (
	"errors"

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

// Session is the whole contract between the engine and any frontend.
// A future remote (daemon) implementation can satisfy it unchanged.
type Session interface {
	// Send starts a turn. It returns immediately; progress arrives on Events.
	Send(prompt string) error
	// Events delivers progress. Consumers MUST keep draining it. Each Send
	// ends with exactly one terminal event (done, error or cancelled).
	Events() <-chan Event
	// Respond answers an ApprovalRequested event.
	Respond(approvalID string, d Decision) error
	// Cancel aborts the running turn, if any.
	Cancel()
	// Close cancels, waits for the turn to stop, then closes Events.
	Close()
}

type Config struct {
	Provider  llm.Provider
	Tools     *tools.Registry
	Policy    permission.Policy
	System    string
	MaxTokens int
	MaxTurns  int // hard cap on model calls per Send; guards runaway tool loops
}
