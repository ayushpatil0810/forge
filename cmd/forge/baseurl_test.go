package main

import (
	"strings"
	"testing"
)

func TestConfig_RefusesNonLoopbackHTTPBaseURL(t *testing.T) {
	resetCLIFlags()
	t.Cleanup(resetCLIFlags)
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("FORGE_BASE_URL", "")
	t.Setenv("FORGE_PROVIDER", "")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"config", "--provider", "openai", "--base-url", "http://203.0.113.10/v1"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "203.0.113.10") {
		t.Fatalf("expected refusal, got %v", err)
	}

	resetCLIFlags()
	cmd = newRootCmd()
	cmd.SetArgs([]string{"config", "--provider", "openai", "--base-url", "http://127.0.0.1:11434/v1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("loopback http should be allowed: %v", err)
	}

	resetCLIFlags()
	cmd = newRootCmd()
	cmd.SetArgs([]string{"config", "--provider", "openai", "--base-url", "http://203.0.113.10/v1", "--allow-insecure-base-url"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--allow-insecure-base-url should permit the URL: %v", err)
	}
}
