//go:build windows

package tools

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetProcessHandleCount = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount")

func processHandleCount(t *testing.T) uint32 {
	t.Helper()
	var count uint32
	r, _, err := procGetProcessHandleCount.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&count)))
	if r == 0 {
		t.Fatalf("GetProcessHandleCount: %v", err)
	}
	return count
}

func TestJobHandleDoesNotGrow(t *testing.T) {
	tool := Bash{Root: t.TempDir(), Shell: DetectShell("")}
	input, _ := json.Marshal(map[string]any{"command": "echo ok", "timeout_seconds": 10})
	runtime.GC()
	before := processHandleCount(t)
	for i := 0; i < 100; i++ {
		res, err := tool.Run(context.Background(), input)
		if err != nil || res.IsError {
			t.Fatalf("command %d: %v %s", i, err, res.Output)
		}
	}
	runtime.GC()
	after := processHandleCount(t)
	if after > before+30 {
		t.Fatalf("handle count grew from %d to %d across 100 commands", before, after)
	}
}
