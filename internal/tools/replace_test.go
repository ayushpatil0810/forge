package tools

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReplaceFile_ReadOnlyDestinationSurvives(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dest.txt")
	original := []byte("original\r\n")
	if err := os.WriteFile(dst, original, 0o644); err != nil {
		t.Fatal(err)
	}

	var err error
	if runtime.GOOS == "windows" {
		// A read-only attribute blocks MoveFileEx. The old fallback deleted or
		// truncated the destination before the new file was in place.
		if err = os.Chmod(dst, 0o444); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dst, 0o644) })
		err = atomicWriteFile(dst, []byte("replaced"), 0o644)
	} else {
		// POSIX rename replaces a read-only file when the directory is writable.
		// Make the directory read-only after both names exist so rename fails.
		src := filepath.Join(dir, "incoming.txt")
		if werr := os.WriteFile(src, []byte("replaced"), 0o644); werr != nil {
			t.Fatal(werr)
		}
		if cerr := os.Chmod(dir, 0o555); cerr != nil {
			t.Fatal(cerr)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		err = replaceFile(src, dst)
		_ = os.Chmod(dir, 0o755)
		_ = os.Remove(src)
	}
	got, readErr := os.ReadFile(dst)
	if readErr != nil {
		t.Fatalf("destination missing after failed replace: %v (replace err=%v)", readErr, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("destination changed to %q (replace err=%v)", got, err)
	}
	if err == nil {
		t.Fatal("expected replace to fail and leave the destination in place")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".forge_tmp_*"))
	if len(leftovers) != 0 {
		t.Fatalf("temp file left behind: %v", leftovers)
	}
}

func TestReplaceFile_PreservesBOMAndCRLF(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dest.txt")
	if err := os.WriteFile(dst, []byte("old\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := []byte("\xef\xbb\xbfhello\r\nworld\r\n")
	if err := atomicWriteFile(dst, want, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
