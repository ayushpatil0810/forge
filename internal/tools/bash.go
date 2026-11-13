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
	Root     string
	Shell    ShellConfig
	EnvAllow []string
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

func sanitizeEnvironment(environ []string, allow []string) []string {
	allowed := make(map[string]struct{}, len(allow))
	for _, name := range allow {
		allowed[strings.ToUpper(name)] = struct{}{}
	}
	var clean []string
	for _, kv := range environ {
		idx := strings.IndexByte(kv, '=')
		if idx == -1 {
			continue
		}
		key := kv[:idx]
		if _, ok := allowed[strings.ToUpper(key)]; ok {
			clean = append(clean, kv)
			continue
		}
		if sensitiveEnv(key) {
			continue
		}
		clean = append(clean, kv)
	}
	clean = append(clean, "CI=1", "NO_COLOR=1")
	return clean
}

func sensitiveEnv(key string) bool {
	kUpper := strings.ToUpper(key)
	suffixes := []string{
		"_API_KEY", "_TOKEN", "_SECRET", "_PASSWORD", "_PASSWD", "_PWD",
		"_CREDENTIALS", "_PRIVATE_KEY", "_ACCESS_KEY", "_SECRET_KEY",
	}
	for _, suf := range suffixes {
		if strings.HasSuffix(kUpper, suf) {
			return true
		}
	}
	prefixes := []string{"AWS_", "AZURE_", "GCP_", "GH_", "GITHUB_", "NPM_"}
	for _, pre := range prefixes {
		if strings.HasPrefix(kUpper, pre) {
			return true
		}
	}
	switch kUpper {
	case "GOOGLE_APPLICATION_CREDENTIALS", "PGPASSWORD", "MYSQL_PWD", "SSH_AUTH_SOCK", "DATABASE_URL",
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY":
		return true
	default:
		return false
	}
}

// streamingBuffer keeps a 10 KB head and a rolling 20 KB tail. Bytes that roll
// off the tail are counted and not retained, so a command that prints tens of
// megabytes cannot grow this buffer.
type streamingBuffer struct {
	mu       sync.Mutex
	head     []byte
	tail     []byte
	dropped  int
	stream   StreamFunc
	streamed int
}

func (s *streamingBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(p)
	if n == 0 {
		return 0, nil
	}
	if s.stream != nil && s.streamed < MaxBashOutputBytes {
		remain := MaxBashOutputBytes - s.streamed
		chunk := p
		if len(chunk) > remain {
			chunk = chunk[:remain]
		}
		s.stream(string(chunk))
		s.streamed += len(chunk)
	}
	if len(s.head) < HeadBashBytes {
		take := HeadBashBytes - len(s.head)
		if take > len(p) {
			take = len(p)
		}
		s.head = append(s.head, p[:take]...)
		p = p[take:]
	}
	if len(p) == 0 {
		return n, nil
	}
	s.tail = append(s.tail, p...)
	if len(s.tail) > TailBashBytes {
		overflow := len(s.tail) - TailBashBytes
		s.dropped += overflow
		next := make([]byte, TailBashBytes)
		copy(next, s.tail[overflow:])
		s.tail = next
	}
	return n, nil
}

func (s *streamingBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dropped == 0 {
		return string(s.head) + string(s.tail)
	}
	return fmt.Sprintf("%s\n... [dropped %d bytes] ...\n%s", s.head, s.dropped, s.tail)
}

func (s *streamingBuffer) Dropped() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

func (s *streamingBuffer) Retained() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.head) + len(s.tail)
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
	cmd.Env = sanitizeEnvironment(os.Environ(), b.EnvAllow)
	cmd.Stdin = bytes.NewReader(nil)

	// Setup process group / job object for clean process-tree termination.
	// On Windows the process is created suspended and only resumed after it is
	// inside the job. Assignment failure aborts the command.
	pg, err := setupProcessGroup(cmd)
	if err != nil {
		return Result{Output: fmt.Sprintf("failed to create process group: %s", err), IsError: true}, nil
	}
	defer pg.Close()

	streamFn := StreamFromContext(ctx)
	outBuf := &streamingBuffer{stream: streamFn}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		return Result{Output: fmt.Sprintf("failed to start shell process (%s): %s", sh.Path, err), IsError: true}, nil
	}

	if err := pg.Attach(cmd); err != nil {
		pg.Kill(cmd)
		return Result{Output: fmt.Sprintf("failed to contain shell process: %s", err), IsError: true}, nil
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
	finalOutput := cleanOutput
	if outBuf.Dropped() == 0 {
		finalOutput = TruncateHeadTail(cleanOutput, MaxBashOutputBytes, HeadBashBytes, TailBashBytes)
	}

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
