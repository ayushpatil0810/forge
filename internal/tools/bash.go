package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBashTimeout = 120 // seconds
	MaxBashTimeout     = 600 // seconds
	MaxBashOutputBytes = 30 * 1024
	HeadBashBytes      = 10 * 1024
	TailBashBytes      = 20 * 1024
)

var ansiRegex = regexp.MustCompile(`(?:\x1B[@-Z\\-_]|[\x80-\x9A\x9C-\x9F]|(?:\x1B\[|\x9B)[0-?]*[ -/]*[@-~])`)

// StripANSI removes terminal ANSI escape sequences.
func StripANSI(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}

// TruncateHeadTail keeps the head and tail of output exceeding maxBytes.
func TruncateHeadTail(s string, maxBytes, headBytes, tailBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	if headBytes+tailBytes >= len(s) {
		return s
	}
	head := s[:headBytes]
	tail := s[len(s)-tailBytes:]
	dropped := len(s) - headBytes - tailBytes
	return fmt.Sprintf("%s\n... [dropped %d bytes] ...\n%s", head, dropped, tail)
}

type Bash struct {
	Root  string
	Shell ShellConfig
}

const bashSchema = `{
  "type": "object",
  "properties": {
    "command": {
      "type": "string",
      "description": "The command line string to execute."
    },
    "description": {
      "type": "string",
      "description": "Short human-readable purpose of the command (shown in the UI)."
    },
    "timeout_seconds": {
      "type": "integer",
      "description": "Command timeout in seconds (default 120, max 600)."
    }
  },
  "required": ["command"]
}`

func (Bash) Name() string {
	return "bash"
}

func (b Bash) Description() string {
	sh := b.Shell
	if sh.Path == "" {
		sh = DetectShell("")
	}
	return fmt.Sprintf("Run commands in the host shell (%s, %s). Use for running tests, builds, and inspecting repository state.", sh.Path, sh.Type)
}

func (Bash) Schema() json.RawMessage {
	return json.RawMessage(bashSchema)
}

type bashArgs struct {
	Command        string `json:"command"`
	Description    string `json:"description"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (b Bash) parseArgs(input json.RawMessage) (bashArgs, error) {
	var args bashArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return args, fmt.Errorf("invalid input for bash: %w", err)
	}
	if strings.TrimSpace(args.Command) == "" {
		return args, fmt.Errorf("bash error: 'command' argument cannot be empty")
	}
	if args.TimeoutSeconds <= 0 {
		args.TimeoutSeconds = DefaultBashTimeout
	} else if args.TimeoutSeconds > MaxBashTimeout {
		args.TimeoutSeconds = MaxBashTimeout
	}
	return args, nil
}

func (b Bash) NeedsApproval(input json.RawMessage) ApprovalRequest {
	args, err := b.parseArgs(input)
	if err != nil {
		return ApprovalRequest{}
	}

	workspaceRoot := b.Root
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	absRoot, _ := filepath.Abs(workspaceRoot)

	return ApprovalRequest{
		Description:    args.Description,
		Command:        args.Command,
		Cwd:            absRoot,
		TimeoutSeconds: args.TimeoutSeconds,
	}
}

func sanitizeEnvironment(environ []string) []string {
	var clean []string
	for _, kv := range environ {
		idx := strings.IndexByte(kv, '=')
		if idx == -1 {
			continue
		}
		key := kv[:idx]
		kUpper := strings.ToUpper(key)
		if strings.HasSuffix(kUpper, "_API_KEY") ||
			strings.HasSuffix(kUpper, "_TOKEN") ||
			strings.HasSuffix(kUpper, "_SECRET") ||
			kUpper == "ANTHROPIC_API_KEY" ||
			kUpper == "OPENAI_API_KEY" ||
			kUpper == "GEMINI_API_KEY" ||
			kUpper == "GOOGLE_API_KEY" {
			continue
		}
		clean = append(clean, kv)
	}
	clean = append(clean, "CI=1", "NO_COLOR=1")
	return clean
}

type streamingBuffer struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	stream StreamFunc
}

func (s *streamingBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err = s.buf.Write(p)
	if n > 0 && s.stream != nil {
		s.stream(string(p[:n]))
	}
	return n, err
}

func (s *streamingBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (b Bash) Run(ctx context.Context, input json.RawMessage) (Result, error) {
	args, err := b.parseArgs(input)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}

	workspaceRoot := b.Root
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return Result{Output: fmt.Sprintf("bash error resolving workspace root: %s", err), IsError: true}, nil
	}

	sh := b.Shell
	if sh.Path == "" {
		sh = DetectShell("")
	}

	cmdArgs := sh.BuildArgs(args.Command)
	cmd := exec.Command(sh.Path, cmdArgs...)
	cmd.Dir = absRoot
	cmd.Env = sanitizeEnvironment(os.Environ())
	cmd.Stdin = bytes.NewReader(nil)

	// Setup process group / job object for clean process-tree termination
	pg, err := setupProcessGroup(cmd)
	if err != nil {
		// Log but continue if process group creation is restricted
	}

	streamFn := StreamFromContext(ctx)
	outBuf := &streamingBuffer{stream: streamFn}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		return Result{Output: fmt.Sprintf("failed to start shell process (%s): %s", sh.Path, err), IsError: true}, nil
	}

	if pg != nil {
		_ = pg.Attach(cmd)
	}

	timeoutDur := time.Duration(args.TimeoutSeconds) * time.Second
	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, timeoutDur)
	defer cancelTimeout()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var waitErr error
	var timedOut bool

	select {
	case waitErr = <-done:
		// Process completed normally
	case <-timeoutCtx.Done():
		if pg != nil {
			pg.Kill(cmd)
		} else if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		waitErr = <-done
		if ctx.Err() == nil {
			timedOut = true
		} else {
			return Result{Output: "command cancelled by user", IsError: true}, nil
		}
	}

	rawOutput := outBuf.String()
	cleanOutput := StripANSI(rawOutput)
	finalOutput := TruncateHeadTail(cleanOutput, MaxBashOutputBytes, HeadBashBytes, TailBashBytes)

	if timedOut {
		return Result{
			Output:  fmt.Sprintf("command timed out after %d seconds:\n%s", args.TimeoutSeconds, finalOutput),
			IsError: true,
		}, nil
	}

	if waitErr != nil {
		exitCode := -1
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return Result{
			Output:  fmt.Sprintf("exit status %d:\n%s", exitCode, finalOutput),
			IsError: true,
		}, nil
	}

	return Result{Output: finalOutput}, nil
}
