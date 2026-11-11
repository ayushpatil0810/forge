package tools

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type ShellType string

const (
	ShellPwsh       ShellType = "pwsh"
	ShellPowerShell ShellType = "powershell"
	ShellCmd        ShellType = "cmd"
	ShellPOSIX      ShellType = "posix"
)

// ShellConfig encapsulates shell binary and execution arguments.
type ShellConfig struct {
	Path string
	Type ShellType
}

// DetectShell detects the native shell or uses the provided override.
// On Windows: pwsh -> powershell.exe -> cmd.exe.
// On Linux/macOS: $SHELL -> /bin/sh.
func DetectShell(override string) ShellConfig {
	if override != "" {
		lower := strings.ToLower(override)
		if strings.Contains(lower, "pwsh") {
			return ShellConfig{Path: override, Type: ShellPwsh}
		}
		if strings.Contains(lower, "powershell") {
			return ShellConfig{Path: override, Type: ShellPowerShell}
		}
		if strings.Contains(lower, "cmd") {
			return ShellConfig{Path: override, Type: ShellCmd}
		}
		return ShellConfig{Path: override, Type: ShellPOSIX}
	}

	if runtime.GOOS == "windows" {
		if path, err := exec.LookPath("pwsh.exe"); err == nil {
			return ShellConfig{Path: path, Type: ShellPwsh}
		}
		if path, err := exec.LookPath("powershell.exe"); err == nil {
			return ShellConfig{Path: path, Type: ShellPowerShell}
		}
		if path, err := exec.LookPath("cmd.exe"); err == nil {
			return ShellConfig{Path: path, Type: ShellCmd}
		}
		return ShellConfig{Path: "powershell.exe", Type: ShellPowerShell}
	}

	if sh := os.Getenv("SHELL"); sh != "" {
		return ShellConfig{Path: sh, Type: ShellPOSIX}
	}
	return ShellConfig{Path: "/bin/sh", Type: ShellPOSIX}
}

// BuildArgs constructs the non-interactive argument slice for running a command string.
func (s ShellConfig) BuildArgs(command string) []string {
	switch s.Type {
	case ShellPwsh, ShellPowerShell:
		return []string{"-NoProfile", "-NonInteractive", "-Command", command}
	case ShellCmd:
		return []string{"/C", command}
	default:
		return []string{"-c", command}
	}
}

