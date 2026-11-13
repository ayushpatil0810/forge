package tools_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/tools"
)

func TestResolveInRoot(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a file inside workspace
	innerFile := filepath.Join(workspace, "hello.txt")
	if err := os.WriteFile(innerFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a file outside workspace
	outsideFile := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		relPath string
		wantErr bool
	}{
		{"root dot", ".", false},
		{"empty string", "", false},
		{"valid inner file", "hello.txt", false},
		{"valid nested path", "sub/dir/file.txt", false},
		{"parent escape ..", "..", true},
		{"parent escape with file", "../secret.txt", true},
		{"deep escape", "../../etc/passwd", true},
		{"absolute outside path", outsideFile, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tools.ResolveInRoot(workspace, tt.relPath)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveInRoot() error = %v, wantErr %v (got %q)", err, tt.wantErr, got)
			}
			if err == nil {
				if !strings.HasPrefix(strings.ToLower(got), strings.ToLower(workspace)) {
					t.Fatalf("expected resolved path %q to be inside %q", got, workspace)
				}
			}
		})
	}
}

func TestResolveInRootSymlinkEscape(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	outsideFile := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	symlinkPath := filepath.Join(workspace, "link.txt")
	err := os.Symlink(outsideFile, symlinkPath)
	if err != nil {
		t.Skipf("skipping symlink test: OS requires privileges to create symlinks: %v", err)
	}

	_, err = tools.ResolveInRoot(workspace, "link.txt")
	if err == nil {
		t.Fatal("expected error resolving symlink that points outside workspace")
	}
}
