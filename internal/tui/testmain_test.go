package tui_test

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ayushpatil0810/forge/internal/credentials"
	"github.com/ayushpatil0810/forge/internal/testutil"
)

func TestMain(m *testing.M) {
	_, cleanup := testutil.IsolateTestEnvironment()
	credentials.ArmForTest()
	_ = os.Unsetenv("NO_COLOR")
	_ = os.Setenv("TERM", "xterm-256color")
	// Fixed profile so golden output does not follow the host terminal.
	// Snapshots compare text after ANSI is stripped.
	lipgloss.SetColorProfile(termenv.TrueColor)
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func TestHomeIsolationGuard(t *testing.T) {
	testutil.AssertIsolatedHome(t)
}
