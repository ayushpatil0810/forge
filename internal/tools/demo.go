package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DemoDelaySpec defines the scripted pauses and pacing for each demo step at --demo-speed 1.0.
type DemoDelaySpec struct {
	Step        string
	Description string
	Duration    time.Duration
}

// DefaultDemoScriptDelays contains the explicit pacing delays for the demo walkthrough (SPEC §10).
// Paced so a human reader can comfortably absorb the thinking, tool calls, and test results.
var DefaultDemoScriptDelays = []DemoDelaySpec{
	{Step: "prompt_submission", Description: "User prompt entered and submitted", Duration: 3 * time.Second},
	{Step: "initial_analysis", Description: "Model analyzes workspace and initiates test run", Duration: 6 * time.Second},
	{Step: "test_run_1", Description: "Simulated bash run of failing go test", Duration: 8 * time.Second},
	{Step: "error_diagnosis", Description: "Model diagnoses boundary error and reads discount.go", Duration: 7 * time.Second},
	{Step: "file_inspection", Description: "Reading discount.go and computing edit", Duration: 6 * time.Second},
	{Step: "diff_presentation", Description: "Presenting diff to user and awaiting approval", Duration: 9 * time.Second},
	{Step: "approval_action", Description: "User approval keystroke (y) and edit execution", Duration: 4 * time.Second},
	{Step: "test_run_2", Description: "Simulated bash re-test passing run", Duration: 8 * time.Second},
	{Step: "conclusion", Description: "Model explains resolution and summarizes changes", Duration: 7 * time.Second},
}

// SumDemoScriptDelays returns the total scripted delay at the given speed multiplier.
// When speed <= 0, delays are 0 (instant execution for tests and CI).
func SumDemoScriptDelays(speed float64) time.Duration {
	if speed <= 0 {
		return 0
	}
	var total time.Duration
	for _, s := range DefaultDemoScriptDelays {
		total += time.Duration(float64(s.Duration) / speed)
	}
	return total
}

// DemoBash is a deterministic, offline-capable executor for the standard demo scenario.
// It allows running the MVP demonstration on fresh machines without requiring
// a local Go compiler or specific shell environment installed on the host.
// edit_file and read_file remain completely real on disk.
type DemoBash struct {
	Root  string
	Speed float64 // 1.0 = normal pacing, 0.0 = instant execution for CI
}

func NewDemoBash(root string, speed float64) DemoBash {
	return DemoBash{
		Root:  root,
		Speed: speed,
	}
}

func (d DemoBash) Name() string {
	return "bash"
}

func (d DemoBash) Description() string {
	return "Run commands in the demo environment."
}

func (d DemoBash) Schema() json.RawMessage {
	return json.RawMessage(bashSchema)
}

func (d DemoBash) NeedsApproval(input json.RawMessage) ApprovalRequest {
	var args bashArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return ApprovalRequest{}
	}
	return ApprovalRequest{
		Description: fmt.Sprintf("Run demo command: %s", args.Command),
		Command:     args.Command,
		Cwd:         d.Root,
	}
}

func (d DemoBash) Run(ctx context.Context, input json.RawMessage) (Result, error) {
	var args bashArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return Result{Output: fmt.Sprintf("invalid input for demo bash: %s", err), IsError: true}, nil
	}

	cmd := strings.TrimSpace(args.Command)

	streamFn := StreamFromContext(ctx)
	// Stream simulation delay if speed > 0
	streamChunk := func(text string) {
		streamFn(text)
		if d.Speed > 0 {
			time.Sleep(time.Duration(15 * float64(time.Millisecond) / d.Speed))
		}
	}

	if strings.Contains(cmd, "go test ./...") || cmd == "go test" {
		out := "--- FAIL: TestDiscount (0.00s)\n    discount_test.go:7: expected 15% discount for orders >= 100, got 0%\nFAIL\nFAIL\texample.com/demo\t0.012s\nFAIL\n"
		for _, line := range strings.SplitAfter(out, "\n") {
			if line != "" {
				streamChunk(line)
			}
		}
		return Result{Output: out, IsError: true}, nil
	}

	if strings.Contains(cmd, "go test -v") || strings.Contains(cmd, "go test") {
		// Verify whether discount.go on disk was actually edited
		discountPath := filepath.Join(d.Root, "discount.go")
		data, err := os.ReadFile(discountPath)
		hasFix := err == nil && (strings.Contains(string(data), "amount >= 100") || strings.Contains(string(data), "100 <="))

		if hasFix {
			out := "=== RUN   TestDiscount\n--- PASS: TestDiscount (0.00s)\nPASS\nok  \texample.com/demo\t0.015s\n"
			for _, line := range strings.SplitAfter(out, "\n") {
				if line != "" {
					streamChunk(line)
				}
			}
			return Result{Output: out, IsError: false}, nil
		}

		out := "=== RUN   TestDiscount\n--- FAIL: TestDiscount (0.00s)\n    discount_test.go:7: expected 15% discount for orders >= 100, got 0%\nFAIL\nFAIL\texample.com/demo\t0.012s\nFAIL\n"
		for _, line := range strings.SplitAfter(out, "\n") {
			if line != "" {
				streamChunk(line)
			}
		}
		return Result{Output: out, IsError: true}, nil
	}

	// Fallback simulated execution
	out := fmt.Sprintf("[demo] executed %s\n", cmd)
	streamChunk(out)
	return Result{Output: out, IsError: false}, nil
}
