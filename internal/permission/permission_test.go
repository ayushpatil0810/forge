package permission_test

import (
	"testing"

	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestPermissionPolicy(t *testing.T) {
	readReq := tools.ApprovalRequest{}
	editReq := tools.ApprovalRequest{Description: "edit", Diff: "-a\n+b\n"}
	bashReq := tools.ApprovalRequest{Description: "run bash"}

	// Default mode: reads free, writes and exec ask
	pDefault := permission.Policy{Mode: permission.ModeDefault}
	if pDefault.Check("read_file", readReq) != permission.Allow {
		t.Fatal("expected read allowed in default mode")
	}
	if pDefault.Check("edit_file", editReq) != permission.Ask {
		t.Fatal("expected edit to ask in default mode")
	}
	if pDefault.Check("bash", bashReq) != permission.Ask {
		t.Fatal("expected exec to ask in default mode")
	}

	// Plan mode: reads free, writes and exec denied
	pPlan := permission.Policy{Mode: permission.ModePlan}
	if pPlan.Check("read_file", readReq) != permission.Allow {
		t.Fatal("expected read allowed in plan mode")
	}
	if pPlan.Check("edit_file", editReq) != permission.Deny {
		t.Fatal("expected edit denied in plan mode")
	}
	if pPlan.Check("bash", bashReq) != permission.Deny {
		t.Fatal("expected exec denied in plan mode")
	}

	// AcceptEdits mode: reads free, edits free, exec asks
	pEdits := permission.Policy{Mode: permission.ModeAcceptEdits}
	if pEdits.Check("read_file", readReq) != permission.Allow {
		t.Fatal("expected read allowed in accept-edits mode")
	}
	if pEdits.Check("edit_file", editReq) != permission.Allow {
		t.Fatal("expected edit allowed in accept-edits mode")
	}
	if pEdits.Check("bash", bashReq) != permission.Ask {
		t.Fatal("expected exec to ask in accept-edits mode")
	}

	// YOLO mode: everything allowed except dangerous commands
	pYOLO := permission.Policy{Mode: permission.ModeYOLO}
	if pYOLO.Check("read_file", readReq) != permission.Allow {
		t.Fatal("expected read allowed in yolo mode")
	}
	if pYOLO.Check("edit_file", editReq) != permission.Allow {
		t.Fatal("expected edit allowed in yolo mode")
	}
	if pYOLO.Check("bash", bashReq) != permission.Allow {
		t.Fatal("expected bash allowed in yolo mode")
	}
}

func TestModeByToolMatrix(t *testing.T) {
	type toolCase struct {
		name string
		req  tools.ApprovalRequest
	}

	toolCases := []toolCase{
		{
			name: "read_tool",
			req:  tools.ApprovalRequest{}, // empty: no approval needed
		},
		{
			name: "edit_file_normal",
			req:  tools.ApprovalRequest{Description: "edit main.go", Diff: "-old\n+new\n"},
		},
		{
			name: "edit_file_protected",
			req:  tools.ApprovalRequest{Description: "edit .git/config", Diff: "-old\n+new\n", IsProtected: true},
		},
		{
			name: "write_file_normal",
			req:  tools.ApprovalRequest{Description: "write new.go", Diff: "+content\n"},
		},
		{
			name: "bash_normal",
			req:  tools.ApprovalRequest{Description: "go test ./...", Command: "go test ./..."},
		},
		{
			name: "bash_flagged_dangerous",
			req:  tools.ApprovalRequest{Description: "rm -rf /", Command: "rm -rf /", IsDangerous: true},
		},
	}

	// Expected matrix: [mode][toolCase.name] -> Verdict
	expected := map[permission.Mode]map[string]permission.Verdict{
		permission.ModeDefault: {
			"read_tool":              permission.Allow,
			"edit_file_normal":       permission.Ask,
			"edit_file_protected":    permission.Ask,
			"write_file_normal":      permission.Ask,
			"bash_normal":            permission.Ask,
			"bash_flagged_dangerous": permission.Ask,
		},
		permission.ModeAcceptEdits: {
			"read_tool":              permission.Allow,
			"edit_file_normal":       permission.Allow,
			"edit_file_protected":    permission.Ask,
			"write_file_normal":      permission.Allow,
			"bash_normal":            permission.Ask,
			"bash_flagged_dangerous": permission.Ask,
		},
		permission.ModePlan: {
			"read_tool":              permission.Allow,
			"edit_file_normal":       permission.Deny,
			"edit_file_protected":    permission.Deny,
			"write_file_normal":      permission.Deny,
			"bash_normal":            permission.Deny,
			"bash_flagged_dangerous": permission.Deny,
		},
		permission.ModeYOLO: {
			"read_tool":              permission.Allow,
			"edit_file_normal":       permission.Allow,
			"edit_file_protected":    permission.Ask, // Protected paths ask even in YOLO (P-6)
			"write_file_normal":      permission.Allow,
			"bash_normal":            permission.Allow,
			"bash_flagged_dangerous": permission.Ask, // Dangerous commands ask even in YOLO!
		},
	}

	modes := []permission.Mode{
		permission.ModeDefault,
		permission.ModeAcceptEdits,
		permission.ModePlan,
		permission.ModeYOLO,
	}

	for _, mode := range modes {
		policy := permission.Policy{Mode: mode}
		for _, tc := range toolCases {
			t.Run(string(mode)+"_"+tc.name, func(t *testing.T) {
				want := expected[mode][tc.name]
				got := policy.Check(tc.name, tc.req)
				if got != want {
					t.Fatalf("Policy{Mode: %s}.Check(%s) = %v; want %v", mode, tc.name, got, want)
				}
			})
		}
	}
}

func TestProtectedPathsByModeTable(t *testing.T) {
	pathClasses := []struct {
		name        string
		path        string
		isProtected bool
	}{
		{name: "normal_code", path: "src/main.go", isProtected: false},
		{name: "normal_doc", path: "docs/readme.txt", isProtected: false},
		{name: "git_dir", path: ".git/config", isProtected: true},
		{name: "git_subpath", path: "sub/.git/HEAD", isProtected: true},
		{name: "env_exact", path: ".env", isProtected: true},
		{name: "env_local", path: ".env.local", isProtected: true},
		{name: "env_prod", path: ".env.production", isProtected: true},
		{name: "ssh_key", path: ".ssh/id_rsa", isProtected: true},
		{name: "ssh_config", path: ".ssh/config", isProtected: true},
		{name: "aws_credentials", path: ".aws/credentials", isProtected: true},
		{name: "aws_config", path: ".aws/config", isProtected: true},
		{name: "pem_cert", path: "certs/server.pem", isProtected: true},
		{name: "tls_key", path: "certs/private.key", isProtected: true},
	}

	modes := []permission.Mode{
		permission.ModeDefault,
		permission.ModeAcceptEdits,
		permission.ModeYOLO,
		permission.ModePlan,
	}

	for _, pc := range pathClasses {
		// First verify IsProtectedPath classifier
		if got := tools.IsProtectedPath(pc.path); got != pc.isProtected {
			t.Errorf("IsProtectedPath(%q) = %v; want %v", pc.path, got, pc.isProtected)
		}

		for _, mode := range modes {
			t.Run(string(mode)+"_"+pc.name, func(t *testing.T) {
				policy := permission.Policy{Mode: mode}
				req := tools.ApprovalRequest{
					Description: "Edit " + pc.path,
					Diff:        "-old\n+new\n",
					IsProtected: tools.IsProtectedPath(pc.path),
				}

				verdict := policy.Check("edit_file", req)

				switch mode {
				case permission.ModePlan:
					if verdict != permission.Deny {
						t.Errorf("ModePlan for %s must be Deny, got %v", pc.path, verdict)
					}
				case permission.ModeDefault:
					if verdict != permission.Ask {
						t.Errorf("ModeDefault for %s must be Ask, got %v", pc.path, verdict)
					}
				case permission.ModeAcceptEdits:
					if pc.isProtected {
						if verdict != permission.Ask {
							t.Errorf("ModeAcceptEdits for protected %s must be Ask, got %v", pc.path, verdict)
						}
					} else {
						if verdict != permission.Allow {
							t.Errorf("ModeAcceptEdits for non-protected %s must be Allow, got %v", pc.path, verdict)
						}
					}
				case permission.ModeYOLO:
					if pc.isProtected {
						if verdict != permission.Ask {
							t.Errorf("ModeYOLO for protected %s must be Ask, got %v", pc.path, verdict)
						}
					} else {
						if verdict != permission.Allow {
							t.Errorf("ModeYOLO for non-protected %s must be Allow, got %v", pc.path, verdict)
						}
					}
				}
			})
		}
	}
}
