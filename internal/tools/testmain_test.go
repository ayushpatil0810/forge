package tools_test

import (
	"os"
	"testing"

	"github.com/ayushpatil0810/forge/internal/testutil"
)

func TestMain(m *testing.M) {
	_, cleanup := testutil.IsolateTestEnvironment()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func TestHomeIsolationGuard(t *testing.T) {
	testutil.AssertIsolatedHome(t)
}
