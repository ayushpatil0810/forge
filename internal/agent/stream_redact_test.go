package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/llm/mock"
	"github.com/ayushpatil0810/forge/internal/redact"
	"github.com/ayushpatil0810/forge/internal/tools"
)

type chunkingTool struct{}

func (chunkingTool) Name() string        { return "stream_probe" }
func (chunkingTool) Description() string { return "streams a secret in tiny chunks" }
func (chunkingTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (chunkingTool) NeedsApproval(json.RawMessage) tools.ApprovalRequest {
	return tools.ApprovalRequest{}
}
func (chunkingTool) Run(ctx context.Context, _ json.RawMessage) (tools.Result, error) {
	stream := tools.StreamFromContext(ctx)
	payload := streamProbePayload()
	for i := 0; i < len(payload); i++ {
		stream(payload[i : i+1])
	}
	return tools.Result{Output: payload}, nil
}

func streamProbePayload() string {
	return "prefix " + streamProbeSecret + " middle " + streamProbeKey + " suffix"
}

const (
	streamProbeSecret = "super-secret-value"
	streamProbeKey    = "sk-abcdefghijklmnopqrstuvwxyz123456"
)

type credentialFixtureTool struct {
	value string
}

func (credentialFixtureTool) Name() string        { return "credential_fixture" }
func (credentialFixtureTool) Description() string { return "checks local test input" }
func (credentialFixtureTool) Schema() json.RawMessage {
	return json.RawMessage("{\"type\":\"object\"}")
}
func (t credentialFixtureTool) NeedsApproval(json.RawMessage) tools.ApprovalRequest {
	return tools.ApprovalRequest{Description: "Review test input", Diff: "+" + t.value}
}
func (t credentialFixtureTool) Run(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var args struct{ Text string }
	if err := json.Unmarshal(input, &args); err != nil || args.Text != t.value {
		return tools.Result{}, errors.New("wrong test input")
	}
	return tools.Result{}, errors.New("fixture diagnostic: " + args.Text)
}

func TestCredentialRedactionAcrossEventsAndHistory(t *testing.T) {
	redact.ClearSecrets()
	t.Cleanup(redact.ClearSecrets)
	const key = "fixture \"credential\" value"
	agent.RegisterSecret(key)
	split := len("fixture ")
	p := mock.New(
		mock.Turn{
			mock.Text("response " + key[:split]), mock.Text(key[split:] + " shown"),
			{Type: llm.StreamThinking, Text: "thought " + key[:split]},
			{Type: llm.StreamThinking, Text: key[split:]},
			mock.ToolUse(key, "credential_fixture", map[string]any{"text": key}),
		},
		mock.Turn{mock.Text("done")},
	)
	s := agent.NewLocal(agent.Config{Provider: p, Tools: tools.NewRegistry(credentialFixtureTool{value: key}), System: key})
	defer s.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := s.Send(ctx, "user prompt "+key); err != nil {
		t.Fatal(err)
	}
	var text, thoughts strings.Builder
	var callID string
	var approved, result bool
	for done := false; !done; {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case agent.EventTokenDelta:
				text.WriteString(ev.Token.Text)
			case agent.EventThinkingDelta:
				thoughts.WriteString(ev.Thinking.Thinking)
			case agent.EventToolStarted:
				callID = ev.ToolStart.ID
				if callID == "" || callID == key || !json.Valid([]byte(ev.ToolStart.Input)) || !strings.Contains(ev.ToolStart.Input, "[REDACTED:secret]") {
					t.Fatal("tool event did not sanitize the ID and JSON input")
				}
			case agent.EventApprovalRequested:
				approved = ev.Approval.IsProtected && ev.Approval.Diff == "+[REDACTED:secret]"
				if err := s.Respond(ev.Approval.ID, agent.DecisionAllow); err != nil {
					t.Fatal(err)
				}
			case agent.EventToolFinished:
				result = ev.ToolDone.ID == callID && ev.ToolDone.Error == "fixture diagnostic: [REDACTED:secret]"
			}
			done = ev.Terminal()
		case <-ctx.Done():
			t.Fatal("session did not finish")
		}
	}
	if text.String() != "response [REDACTED:secret] showndone" || thoughts.String() != "thought [REDACTED:secret]" || !approved || !result {
		t.Fatal("stream, approval, or tool diagnostic was not redacted as expected")
	}
	if len(p.Requests) != 2 || p.Requests[0].System != "[REDACTED:secret]" {
		t.Fatal("provider request did not use a redacted system prompt")
	}
	for _, messages := range [][]llm.Message{s.History(), p.Requests[1].Messages} {
		if len(messages) < 3 || messages[0].Text != "user prompt [REDACTED:secret]" ||
			messages[1].Text != "response [REDACTED:secret] shown" ||
			!strings.Contains(string(messages[1].ToolCalls[0].Input), "[REDACTED:secret]") ||
			messages[2].ToolCallID != callID || messages[2].Text != "fixture diagnostic: [REDACTED:secret]" {
			t.Fatal("sanitized conversation did not preserve the tool pairing and expected messages")
		}
	}
}

func TestToolOutputDelta_RedactsSplitSecrets(t *testing.T) {
	redact.ClearSecrets()
	t.Cleanup(redact.ClearSecrets)
	agent.RegisterSecret(streamProbeSecret)

	p := mock.New(
		mock.Turn{
			mock.ToolUse("call-stream", "stream_probe", map[string]any{}),
		},
		mock.Turn{mock.Text("done")},
	)
	s := agent.NewLocal(agent.Config{
		Provider: p,
		Tools:    tools.NewRegistry(chunkingTool{}),
	})
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Send(ctx, "stream it"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var deltas []string
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == agent.EventToolOutputDelta && ev.ToolOut != nil {
				deltas = append(deltas, ev.ToolOut.Chunk)
				if strings.Contains(ev.ToolOut.Chunk, streamProbeSecret) || strings.Contains(ev.ToolOut.Chunk, streamProbeKey) {
					t.Fatalf("secret leaked in delta %q", ev.ToolOut.Chunk)
				}
			}
			if ev.Type == agent.EventToolFinished && ev.ToolDone != nil {
				if strings.Contains(ev.ToolDone.Result, streamProbeSecret) || strings.Contains(ev.ToolDone.Result, streamProbeKey) {
					t.Fatalf("secret leaked in final result %q", ev.ToolDone.Result)
				}
			}
			if ev.Terminal() {
				goto done
			}
		case <-deadline:
			t.Fatal("timed out waiting for stream events")
		}
	}
done:
	if len(deltas) == 0 {
		t.Fatal("expected at least one redacted delta")
	}
	joined := strings.Join(deltas, "")
	if strings.Contains(joined, streamProbeSecret) || strings.Contains(joined, streamProbeKey) {
		t.Fatalf("secret leaked across deltas: %q", joined)
	}
	if !strings.Contains(joined, "[REDACTED:") {
		t.Fatalf("deltas were not redacted: %q", joined)
	}
}
