package tools_test

import (
	"runtime"
	"testing"

	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestTrailingDotAndSpaceClassification(t *testing.T) {
	windowsOS := runtime.GOOS == "windows"
	cases := []struct {
		path string
		want bool
	}{
		{path: "secret.pem.", want: windowsOS},
		{path: "secret.pem ", want: windowsOS},
		{path: ".env.", want: windowsOS},
		{path: "secret.pem", want: true},
		{path: "notes.txt.", want: false},
	}
	for _, tc := range cases {
		got := tools.IsProtectedPath(tc.path)
		if got != tc.want {
			t.Errorf("IsProtectedPath(%q) = %v, want %v (GOOS=%s)", tc.path, got, tc.want, runtime.GOOS)
		}
	}
}
