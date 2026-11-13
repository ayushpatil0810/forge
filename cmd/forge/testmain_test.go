package main

import (
	"os"
	"testing"

	"github.com/ayushpatil0810/forge/internal/credentials"
)

func TestMain(m *testing.M) {
	credentials.ArmForTest()
	os.Exit(m.Run())
}
