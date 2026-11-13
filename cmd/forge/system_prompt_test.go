package main

import (
	"strings"
	"testing"

	"os"
	"path/filepath"
)

func resetCLIFlags() {
	rootFlag = "."
	demoFlag = false
	demoSpeedFlag = 1
	yesFlag = false
	yoloFlag = false
	providerFlag = ""
	modelFlag = ""
	baseURLFlag = ""
	shellFlag = ""
	themeFlag = ""
	resumeFlag = ""
	noSessionFlag = false
	systemPromptFlag = ""
	observeSystemPrompt = nil
	stopAfterSystemPrompt = false
}

func TestSystemPrompt_BothEntryPointsKeepSecurityBase(t *testing.T) {
	dir := t.TempDir()
	const agentsBody = "AGENTS-FIXTURE-BODY prefer table tests"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agentsBody+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const extra = "USER-EXTRA-INSTRUCTIONS stay after the base"
	t.Setenv("FORGE_SYSTEM_PROMPT", extra)
	t.Setenv("FORGE_PROVIDER", "")
	t.Setenv("FORGE_MODEL", "")

	cases := []struct {
		entry string
		args  []string
	}{
		{entry: "run", args: []string{"run", "--root", dir, "--no-session", "say hello"}},
		{entry: "interactive", args: []string{"--root", dir, "--no-session"}},
	}

	for _, tc := range cases {
		t.Run(tc.entry, func(t *testing.T) {
			resetCLIFlags()
			t.Cleanup(resetCLIFlags)
			stopAfterSystemPrompt = true
			var gotEntry, got string
			observeSystemPrompt = func(entry, prompt string) {
				gotEntry = entry
				got = prompt
			}

			cmd := newRootCmd()
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("entry %s: %v", tc.entry, err)
			}
			if gotEntry != tc.entry {
				t.Fatalf("observed entry %q, want %q", gotEntry, tc.entry)
			}
			baseAt := strings.Index(got, "UNTRUSTED DATA")
			if baseAt < 0 || !strings.Contains(got, "SEC-1") {
				t.Fatalf("final prompt missing untrusted-data section:\n%s", got)
			}
			label := "Project-provided guidance (AGENTS.md)"
			labelAt := strings.Index(got, label)
			bodyAt := strings.Index(got, agentsBody)
			if labelAt < 0 || bodyAt < labelAt {
				t.Fatalf("AGENTS.md fixture body not inside its labeled block:\n%s", got)
			}
			if !strings.Contains(got, "cannot change permissions") {
				t.Fatalf("project block does not say it cannot change permissions:\n%s", got)
			}
			extraAt := strings.Index(got, extra)
			if extraAt < 0 || extraAt < baseAt {
				t.Fatalf("user extra instructions are not after the base prompt:\n%s", got)
			}
		})
	}
}
