package tools_test

import (
	"runtime"
	"testing"

	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/policy"
	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestProtectedPathClassifierTable(t *testing.T) {
	foldEnv := runtime.GOOS == "windows" || runtime.GOOS == "darwin"
	cases := []struct {
		path string
		want bool
	}{
		{path: ".GIT/config", want: true},
		{path: "vendor/lib/.git/config", want: true},
		{path: "vendor/lib/.GIT/config", want: true},
		{path: ".Env", want: foldEnv},
		{path: "Foo/.SSH/id_rsa", want: true},
		{path: ".env", want: true},
		{path: ".env.local", want: true},
		{path: ".gnupg/pubring.kbx", want: true},
		{path: "certs/server.pem", want: true},
		{path: "certs/private.key", want: true},
		{path: "certs/store.pfx", want: true},
		{path: "certs/store.p12", want: true},
		{path: "id_ed25519.pub", want: true},
		{path: ".npmrc", want: true},
		{path: ".pypirc", want: true},
		{path: ".netrc", want: true},
		{path: "credentials", want: true},
		{path: "src/main.go", want: false},
		{path: ".environment", want: false},
		{path: "notes.txt", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := tools.IsProtectedPath(tc.path); got != tc.want {
				t.Fatalf("tools.IsProtectedPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
			if got := policy.IsProtectedPath(tc.path); got != tc.want {
				t.Fatalf("policy.IsProtectedPath(%q) = %v, want %v (classifier diverged)", tc.path, got, tc.want)
			}
			res := policy.Evaluate(policy.EvalRequest{
				ToolName: "read_file",
				Path:     tc.path,
				Mode:     permission.ModeYOLO,
			}, &policy.PolicySet{})
			if tc.want && !res.IsFloor {
				t.Fatalf("policy floor did not use the classifier for %q: %+v", tc.path, res)
			}
			if !tc.want && res.IsFloor {
				t.Fatalf("policy floor flagged unprotected path %q: %+v", tc.path, res)
			}
		})
	}
}
