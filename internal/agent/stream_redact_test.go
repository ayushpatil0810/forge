package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
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
