package tui_test

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ayushpatil0810/forge/internal/credentials"
)

func TestMain(m *testing.M) {
	credentials.ArmForTest()
	_ = os.Unsetenv("NO_COLOR")
	_ = os.Setenv("TERM", "xterm-256color")
	// Fixed profile so golden output does not follow the host terminal.
	// Snapshots compare text after ANSI is stripped.
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}
