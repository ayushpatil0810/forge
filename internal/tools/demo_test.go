package tools_test

import (
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestDemoScriptPacing(t *testing.T) {
	// SPEC §10 requires the demo to run in under 2 minutes.
	// Pacing ensures human readability, asserting total delay at speed 1.0 is strictly between 45s and 75s.
	totalDelay := tools.SumDemoScriptDelays(1.0)

	minAllowed := 45 * time.Second
	maxAllowed := 75 * time.Second

	if totalDelay <= minAllowed || totalDelay >= maxAllowed {
		t.Fatalf("expected sum of demo scripted delays to be strictly between %v and %v, got %v", minAllowed, maxAllowed, totalDelay)
	}

	// At speed 0 (CI / testing mode), delays must be instantaneous
	if instant := tools.SumDemoScriptDelays(0.0); instant != 0 {
		t.Fatalf("expected 0 duration for instant demo speed, got %v", instant)
	}
}
