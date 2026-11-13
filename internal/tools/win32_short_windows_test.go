//go:build windows

package tools_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/tools"
	"golang.org/x/sys/windows"
)

func TestShortNameResolvesToProtectedLongName(t *testing.T) {
	ws := t.TempDir()
	longName := "id_ed25519_extra"
	full := filepath.Join(ws, longName)
	if err := os.WriteFile(full, []byte("not-a-real-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	short, err := shortPathName(full)
	if err != nil || short == "" || strings.EqualFold(filepath.Clean(short), filepath.Clean(full)) {
		t.Skipf("8.3 short name was not created for %s (err=%v short=%q); not testable on this volume", longName, err, short)
	}
	rel, err := filepath.Rel(ws, short)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := tools.ResolveInRoot(ws, rel)
	if err != nil {
		t.Fatalf("ResolveInRoot(%q): %v", rel, err)
	}
	if !strings.EqualFold(filepath.Base(resolved), longName) {
		t.Fatalf("short name %q resolved to %q, want base %q", rel, resolved, longName)
	}
	if !tools.IsProtectedPath(resolved) {
		t.Fatalf("long name %q is not classified protected", resolved)
	}
}

func shortPathName(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, 4096)
	n, err := windows.GetShortPathName(p, &buf[0], uint32(len(buf)))
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", os.ErrNotExist
	}
	return windows.UTF16ToString(buf[:n]), nil
}
