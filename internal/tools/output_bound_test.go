package tools

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestBashOutputMemoryStaysBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("prints 50MB")
	}
	sh := DetectShell("")
	tool := Bash{Root: t.TempDir(), Shell: sh}
	var command string
	switch sh.Type {
	case ShellPwsh, ShellPowerShell:
		command = "$s = 'A' * 1048576; foreach ($i in 1..50) { [Console]::Out.Write($s) }"
	default:
		command = "python3 -c 'import sys; sys.stdout.write(\"A\"*52428800)'"
	}
	input, _ := json.Marshal(map[string]any{"command": command, "timeout_seconds": 60})

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	res, err := tool.Run(context.Background(), input)
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("command failed: %s", res.Output)
	}
	if len(res.Output) > 40*1024 {
		t.Fatalf("output retained %d bytes", len(res.Output))
	}
	if !strings.Contains(res.Output, "dropped") {
		t.Fatalf("expected a dropped-byte marker, got %d bytes", len(res.Output))
	}
	if after.HeapAlloc > before.HeapAlloc+16*1024*1024 {
		t.Fatalf("heap grew by %d bytes after a 50MB command", after.HeapAlloc-before.HeapAlloc)
	}
}
