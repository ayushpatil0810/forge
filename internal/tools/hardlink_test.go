package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHardlinkOutsideWorkspaceIsProtected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret-body"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sub, "notes.txt")
	if err := os.Link(target, link); err != nil {
		t.Skipf("hardlinks are not available: %v", err)
	}

	read := ReadFile{Root: root}.NeedsApproval(json.RawMessage(`{"path":"sub/notes.txt"}`))
	if !read.IsProtected {
		t.Fatal("read of a hardlink whose other name is outside the workspace was not protected")
	}
	write := WriteFile{Root: root}.NeedsApproval(json.RawMessage(`{"path":"sub/notes.txt","content":"x"}`))
	if !write.IsProtected {
		t.Fatal("write of a hardlink was not protected")
	}
	edit := EditFile{Root: root}.NeedsApproval(json.RawMessage(`{"path":"sub/notes.txt","old_string":"secret-body","new_string":"x"}`))
	if !edit.IsProtected {
		t.Fatal("edit of a hardlink was not protected")
	}

	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := ReadFile{Root: root}.NeedsApproval(json.RawMessage(`{"path":"readme.txt"}`))
	if plain.IsProtected {
		t.Fatal("a normal single-link file was flagged as protected")
	}
}
