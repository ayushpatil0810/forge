package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHelperProcess serves as a controlled grandchild process for process-tree termination tests.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(0)
	}
	switch args[0] {
	case "grandchild":
		if len(args) > 1 {
			_ = os.WriteFile(args[1], []byte(strconv.Itoa(os.Getpid())), 0644)
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(1)
		}
		defer func() { _ = ln.Close() }()
		_, _ = ln.Accept()
	default:
		os.Exit(0)
	}
}

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
	if !strings.Contains(req.Command, "echo HelloFromBash") {
		t.Fatalf("expected approval command to contain command, got: %s", req.Command)
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

	var sleepCmd string
	switch sh.Type {
	case ShellPwsh, ShellPowerShell:
		sleepCmd = "Start-Sleep -Seconds 10"
	case ShellCmd:
		sleepCmd = "timeout /t 10 /nobreak"
	default:
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

func TestBashTool_SanitizedEnvironment(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY=sk-ant-123",
		"OPENAI_API_KEY=sk-proj-456",
		"GEMINI_API_KEY=gemini-789",
		"GOOGLE_API_KEY=google-abc",
		"CUSTOM_API_KEY=secret_key",
		"GITHUB_TOKEN=ghp_token",
		"AWS_SECRET=secret_aws",
		"AWS_SECRET_ACCESS_KEY=aws-secret",
		"AWS_ACCESS_KEY_ID=aws-id",
		"PGPASSWORD=pg-secret",
		"MYSQL_PWD=mysql-secret",
		"DATABASE_URL=postgres://secret",
		"SSH_AUTH_SOCK=/tmp/ssh",
		"GOOGLE_APPLICATION_CREDENTIALS=/tmp/gcp.json",
		"AZURE_CLIENT_SECRET=azure-secret",
		"GCP_KEY=gcp-secret",
		"GH_TOKEN=gh-secret",
		"NPM_TOKEN=npm-secret",
		"HOME=/home/alice",
		"GOPATH=/go",
		"GOFLAGS=-mod=mod",
		"SAFE_PUBLIC=visible",
		"SAFE_USER=alice",
		"FOO_BAR=baz",
	}

	sanitized := sanitizeEnvironment(env, []string{"AWS_SECRET_ACCESS_KEY"})
	joined := strings.Join(sanitized, "\n")

	for _, bad := range []string{
		"sk-ant-123", "sk-proj-456", "gemini-789", "google-abc",
		"secret_key", "ghp_token", "secret_aws", "aws-id", "pg-secret",
		"mysql-secret", "postgres://secret", "/tmp/ssh", "/tmp/gcp.json",
		"azure-secret", "gcp-secret", "gh-secret", "npm-secret",
	} {
		if strings.Contains(joined, bad) {
			t.Fatalf("sanitized env still contains secret %q: %s", bad, joined)
		}
	}

	if !strings.Contains(joined, "CI=1") {
		t.Fatalf("expected CI=1 in sanitized env")
	}
	if !strings.Contains(joined, "NO_COLOR=1") {
		t.Fatalf("expected NO_COLOR=1 in sanitized env")
	}
	for _, keep := range []string{
		"SAFE_USER=alice", "PATH=/usr/bin", "HOME=/home/alice", "GOPATH=/go",
		"GOFLAGS=-mod=mod", "SAFE_PUBLIC=visible", "AWS_SECRET_ACCESS_KEY=aws-secret",
	} {
		if !strings.Contains(joined, keep) {
			t.Fatalf("expected %s to be kept, got:\n%s", keep, joined)
		}
	}
}

func TestBashTool_ParametersAndStdin(t *testing.T) {
	tempDir := t.TempDir()
	sh := DetectShell("")
	tool := Bash{Root: tempDir, Shell: sh}

	// 1. Default timeout
	args, err := tool.parseArgs(json.RawMessage(`{"command": "echo test"}`))
	if err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}
	if args.TimeoutSeconds != DefaultBashTimeout {
		t.Fatalf("expected default timeout %d, got %d", DefaultBashTimeout, args.TimeoutSeconds)
	}

	// 2. Max timeout clamping
	args, err = tool.parseArgs(json.RawMessage(`{"command": "echo test", "timeout_seconds": 9999}`))
	if err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}
	if args.TimeoutSeconds != MaxBashTimeout {
		t.Fatalf("expected clamped timeout %d, got %d", MaxBashTimeout, args.TimeoutSeconds)
	}

	// 3. Negative timeout
	args, err = tool.parseArgs(json.RawMessage(`{"command": "echo test", "timeout_seconds": -5}`))
	if err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}
	if args.TimeoutSeconds != DefaultBashTimeout {
		t.Fatalf("expected default timeout %d for negative, got %d", DefaultBashTimeout, args.TimeoutSeconds)
	}

	// 4. Stdin is closed (immediately reaches EOF)
	var stdinCmd string
	switch sh.Type {
	case ShellPwsh, ShellPowerShell:
		stdinCmd = "$in = [Console]::In.ReadToEnd(); Write-Host 'done:' $in.Length"
	case ShellCmd:
		stdinCmd = "echo done"
	default:
		stdinCmd = "cat; echo done"
	}

	input, _ := json.Marshal(map[string]any{
		"command":         stdinCmd,
		"timeout_seconds": 5,
	})
	start := time.Now()
	res, err := tool.Run(context.Background(), input)
	duration := time.Since(start)
	if err != nil || res.IsError {
		t.Fatalf("failed running stdin test: %v, res=%+v", err, res)
	}
	if duration > 3*time.Second {
		t.Fatalf("stdin read did not exit promptly on EOF: took %v", duration)
	}
	if !strings.Contains(res.Output, "done") {
		t.Fatalf("expected 'done' in output, got: %s", res.Output)
	}
}

func TestBashTool_ProcessTreeGrandchildKill(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process tree kill test in short mode")
	}

	tempDir := t.TempDir()
	sh := DetectShell("")
	tool := Bash{Root: tempDir, Shell: sh}

	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	// Case 1: Process tree dies on context cancellation
	{
		pidFile := filepath.Join(tempDir, "grandchild_cancel.pid")
		var cmdStr string
		if sh.Type == ShellPwsh || sh.Type == ShellPowerShell {
			cmdStr = fmt.Sprintf("& %q \"-test.run=TestHelperProcess\" \"--\" grandchild %q", os.Args[0], pidFile)
		} else {
			cmdStr = fmt.Sprintf("%q -test.run=TestHelperProcess -- grandchild %q", os.Args[0], pidFile)
		}

		input, _ := json.Marshal(map[string]any{
			"command":         cmdStr,
			"timeout_seconds": 30,
		})

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan bashRun, 1)
		go func() {
			res, err := tool.Run(ctx, input)
			done <- bashRun{res, err}
		}()

		pid := waitForGrandchildPID(t, pidFile, done)
		if !isProcessAlive(pid) {
			t.Fatalf("grandchild %d was expected to be alive before cancel", pid)
		}
		cancel()
		res := awaitBash(t, done)
		if res.err != nil {
			t.Fatalf("unexpected error: %v", res.err)
		}
		if !res.res.IsError || !strings.Contains(res.res.Output, "cancelled by user") {
			t.Fatalf("expected cancelled by user, got: %+v", res.res)
		}
		waitUntilDead(t, pid)
	}

	// Case 2: Process tree dies on timeout
	{
		pidFile := filepath.Join(tempDir, "grandchild_timeout.pid")
		var cmdStr string
		if sh.Type == ShellPwsh || sh.Type == ShellPowerShell {
			cmdStr = fmt.Sprintf("& %q \"-test.run=TestHelperProcess\" \"--\" grandchild %q", os.Args[0], pidFile)
		} else {
			cmdStr = fmt.Sprintf("%q -test.run=TestHelperProcess -- grandchild %q", os.Args[0], pidFile)
		}

		input, _ := json.Marshal(map[string]any{
			"command":         cmdStr,
			"timeout_seconds": 15,
		})

		done := make(chan bashRun, 1)
		go func() {
			res, err := tool.Run(context.Background(), input)
			done <- bashRun{res, err}
		}()
		pid := waitForGrandchildPID(t, pidFile, done)
		res := awaitBash(t, done)
		if res.err != nil {
			t.Fatalf("unexpected error: %v", res.err)
		}
		if !res.res.IsError || !strings.Contains(res.res.Output, "timed out after 15 seconds") {
			t.Fatalf("expected timeout error, got: %+v", res.res)
		}
		waitUntilDead(t, pid)
	}
}

type bashRun struct {
	res Result
	err error
}

func waitForGrandchildPID(t *testing.T, pidFile string, done <-chan bashRun) int {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, _ := strconv.Atoi(strings.TrimSpace(string(data))); pid > 0 {
				return pid
			}
		}
		select {
		case res := <-done:
			t.Fatalf("helper exited before pid file %s appeared: %v output=%q", pidFile, res.err, res.res.Output)
		default:
		}
		if time.Now().After(deadline) {
			select {
			case res := <-done:
				t.Fatalf("helper exited before pid file %s appeared: %v output=%q", pidFile, res.err, res.res.Output)
			default:
				t.Fatalf("pid file %s did not appear within 15s", pidFile)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func awaitBash(t *testing.T, done <-chan bashRun) bashRun {
	t.Helper()
	select {
	case res := <-done:
		return res
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for the shell command to finish")
		return bashRun{}
	}
}

func waitUntilDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("grandchild process %d is still alive after kill", pid)
}
