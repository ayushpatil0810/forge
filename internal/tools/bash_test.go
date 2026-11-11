package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBashTool_ExecutionAndStreaming(t *testing.T) {
	tempDir := t.TempDir()
	sh := DetectShell("")
	tool := Bash{Root: tempDir, Shell: sh}

	var streamedChunks []string
	var mu sync.Mutex

	ctx := ContextWithStream(context.Background(), func(chunk string) {
		mu.Lock()
		defer mu.Unlock()
		streamedChunks = append(streamedChunks, chunk)
	})

	input, _ := json.Marshal(map[string]any{
		"command":     "echo HelloFromBash",
		"description": "test echo",
	})

	req := tool.NeedsApproval(input)
	if !strings.Contains(req.Description, "echo HelloFromBash") {
		t.Fatalf("expected approval description to contain command, got: %s", req.Description)
	}

	res, err := tool.Run(ctx, input)
	if err != nil || res.IsError {
		t.Fatalf("unexpected error running echo: res=%+v, err=%v", res, err)
	}

	if !strings.Contains(res.Output, "HelloFromBash") {
		t.Fatalf("expected output to contain HelloFromBash, got: %s", res.Output)
	}

	mu.Lock()
	defer mu.Unlock()
	combinedStream := strings.Join(streamedChunks, "")
	if !strings.Contains(combinedStream, "HelloFromBash") {
		t.Fatalf("expected streamed chunks to contain HelloFromBash, got: %s", combinedStream)
	}
}

func TestBashTool_TimeoutAndKill(t *testing.T) {
	tempDir := t.TempDir()
	sh := DetectShell("")
	tool := Bash{Root: tempDir, Shell: sh}

	// Run command that sleeps longer than timeout
	var sleepCmd string
	if sh.Type == ShellPwsh || sh.Type == ShellPowerShell {
		sleepCmd = "Start-Sleep -Seconds 10"
	} else if sh.Type == ShellCmd {
		sleepCmd = "timeout /t 10 /nobreak"
	} else {
		sleepCmd = "sleep 10"
	}

	input, _ := json.Marshal(map[string]any{
		"command":         sleepCmd,
		"timeout_seconds": 1,
	})

	start := time.Now()
	res, err := tool.Run(context.Background(), input)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error on timeout")
	}
	if !strings.Contains(res.Output, "timed out after 1 seconds") {
		t.Fatalf("expected timeout output, got: %s", res.Output)
	}
	if duration > 5*time.Second {
		t.Fatalf("process took too long to terminate: %v", duration)
	}
}

func TestBashTool_OutputTruncation(t *testing.T) {
	huge := strings.Repeat("A", 40*1024)
	truncated := TruncateHeadTail(huge, MaxBashOutputBytes, HeadBashBytes, TailBashBytes)
	if len(truncated) >= len(huge) {
		t.Fatalf("expected truncation, got length %d", len(truncated))
	}
	if !strings.Contains(truncated, "[dropped") {
		t.Fatalf("expected dropped marker in truncated output: %s", truncated[:100])
	}
}

func TestBashTool_StripANSI(t *testing.T) {
	colored := "\x1b[31mRed Text\x1b[0m and \x1b[1;32mBold Green\x1b[0m"
	clean := StripANSI(colored)
	if clean != "Red Text and Bold Green" {
		t.Fatalf("StripANSI failed: %q", clean)
	}
}
