package agent_test

import (
	"encoding/json"
	"testing"

	"github.com/ayushpatil0810/forge/internal/agent"
)

func TestEventJSONSerialization(t *testing.T) {
	events := []agent.Event{
		{
			Seq:   1,
			Type:  agent.EventTokenDelta,
			Token: &agent.TokenDeltaPayload{Text: "hello"},
		},
		{
			Seq:      2,
			Type:     agent.EventThinkingDelta,
			Thinking: &agent.ThinkingDeltaPayload{Thinking: "let me think"},
		},
		{
			Seq:  3,
			Type: agent.EventToolStarted,
			ToolStart: &agent.ToolStartedPayload{
				ID:    "t-1",
				Name:  "read_file",
				Input: `{"path":"main.go"}`,
			},
		},
		{
			Seq:  4,
			Type: agent.EventToolOutputDelta,
			ToolOut: &agent.ToolOutputDeltaPayload{
				ID:    "t-1",
				Chunk: "chunk data",
			},
		},
		{
			Seq:  5,
			Type: agent.EventToolFinished,
			ToolDone: &agent.ToolFinishedPayload{
				ID:         "t-1",
				Name:       "read_file",
				Result:     "content",
				DurationMs: 12,
				Status:     agent.ToolOK,
			},
		},
		{
			Seq:  6,
			Type: agent.EventApprovalRequested,
			Approval: &agent.ApprovalRequestedPayload{
				ID:          "t-2",
				Tool:        "edit_file",
				Input:       `{"path":"main.go"}`,
				Description: "replace line 5",
				Diff:        "-old\n+new\n",
			},
		},
		{
			Seq:  7,
			Type: agent.EventCheckpointCreated,
			Checkpt: &agent.CheckpointCreatedPayload{
				ID:    "cp-1",
				Files: []string{"main.go"},
			},
		},
		{
			Seq:  8,
			Type: agent.EventUsageUpdated,
			Usage: &agent.UsageUpdatedPayload{
				InputTokens:  100,
				OutputTokens: 50,
				Cost:         0.002,
			},
		},
		{
			Seq:  9,
			Type: agent.EventError,
			Err: &agent.ErrorPayload{
				Code:      "rate_limited",
				Message:   "too many requests",
				Retryable: true,
			},
		},
		{
			Seq:  10,
			Type: agent.EventDone,
			Done: &agent.DonePayload{
				Reason: "completed",
			},
		},
	}

	for _, ev := range events {
		data, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("failed to marshal event %s: %v", ev.Type, err)
		}
		var decoded agent.Event
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal event %s: %v", ev.Type, err)
		}
		if decoded.Type != ev.Type || decoded.Seq != ev.Seq {
			t.Fatalf("mismatched decoded event: want seq %d type %s, got seq %d type %s", ev.Seq, ev.Type, decoded.Seq, decoded.Type)
		}
	}
}

