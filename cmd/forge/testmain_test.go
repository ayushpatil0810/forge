package main

import (
	"os"
	"testing"

	"github.com/ayushpatil0810/forge/internal/credentials"
	"github.com/ayushpatil0810/forge/internal/testutil"
)

func TestMain(m *testing.M) {
	_, cleanup := testutil.IsolateTestEnvironment()
	credentials.ArmForTest()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func TestHomeIsolationGuard(t *testing.T) {
	testutil.AssertIsolatedHome(t)
}
