package policy

import (
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/permission"
)

func TestSafeFlagAllowAndFloorBypasses(t *testing.T) {
	set := &PolicySet{}
	for _, raw := range []string{"bash(go test:*)", "bash(git:*)", "bash(git status:*)"} {
		parsed, err := ParseRule(ActionAllow, raw, ScopeUser, "user.json", 1)
		if err != nil {
			t.Fatal(err)
		}
		set.UserRules = append(set.UserRules, parsed)
	}

	allowed := []string{"go test ./...", "go test -race -count=1 ./...", "git status", "git diff --stat"}
	for _, cmd := range allowed {
		res := Evaluate(EvalRequest{ToolName: "bash", Command: cmd, Mode: permission.ModeAsk}, set)
		if res.Verdict != permission.Allow {
			t.Errorf("command %q verdict %v (%s), want Allow", cmd, res.Verdict, res.Reason)
		}
	}

	bypasses := []string{
		`go test "-exec" ./evil`,
		`go test --exec ./evil`,
		`go test '-exec' ./evil`,
		`git "-c" core.sshCommand=calc status`,
		`git --config-env=alias.x=EVIL status`,
		`find . -exec calc.exe {} +`,
		`powershell -ec SQBFAFgA`,
		`sh -c "python3 -c 'print(1)'"`,
		`xargs rm`,
		`env GOFLAGS=-toolexec=calc go test ./...`,
	}
	modes := []permission.Mode{permission.ModeAsk, permission.ModeAcceptEdits, permission.ModeYOLO}
	for _, cmd := range bypasses {
		for _, mode := range modes {
			res := Evaluate(EvalRequest{ToolName: "bash", Command: cmd, Mode: mode}, set)
			if res.Verdict == permission.Allow {
				t.Errorf("%s mode allowed floor bypass %q (%s)", mode, cmd, res.Reason)
			}
		}
		headless := Evaluate(EvalRequest{
			ToolName: "bash", Command: cmd, Mode: permission.ModeYOLO, IsHeadless: true,
		}, set)
		if headless.Verdict != permission.Deny {
			t.Errorf("headless yolo verdict for %q = %v, want Deny (%s)", cmd, headless.Verdict, headless.Reason)
		}
	}

	unknown := Evaluate(EvalRequest{
		ToolName: "bash", Command: "go test -coverprofile=out.out ./...", Mode: permission.ModeAsk,
	}, set)
	if unknown.Verdict != permission.Ask {
		t.Errorf("unknown flag verdict %v, want Ask (%s)", unknown.Verdict, unknown.Reason)
	}
}

func TestTokenizerNeverPanicsOnMetacharacters(t *testing.T) {
	samples := []string{";", "|", "&", "$(x)", "`id`", "a%b%", "\"unterminated"}
	for _, s := range samples {
		a := SplitAndAnalyzeCommand(s)
		if strings.ContainsAny(s, ";|&`$") && !strings.Contains(s, "\"") && a.IsSimple {
			t.Fatalf("%q classified simple", s)
		}
	}
}
