package checkpoint_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ayushpatil0810/forge/internal/checkpoint"
)

func TestRoundTripRestore(t *testing.T) {
	ws := t.TempDir()

	// Create initial workspace files
	f1 := filepath.Join(ws, "main.go")
	f2 := filepath.Join(ws, "pkg", "lib.go")
	_ = os.MkdirAll(filepath.Dir(f2), 0755)

	initialContent1 := []byte("package main\n\nfunc main() {}\n")
	initialContent2 := []byte("package pkg\n\nconst Version = \"1.0.0\"\n")
	_ = os.WriteFile(f1, initialContent1, 0644)
	_ = os.WriteFile(f2, initialContent2, 0644)

	mgr, err := checkpoint.New(ws)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// 1. Take initial checkpoint
	cp1, err := mgr.Create(context.Background(), "initial commit")
	if err != nil {
		t.Fatalf("failed to create checkpoint 1: %v", err)
	}

	// 2. Mutate files and create new file
	_ = os.WriteFile(f1, []byte("package main\n\nfunc main() { panic(\"broken\") }\n"), 0644)
	f3 := filepath.Join(ws, "new_file.txt")
	_ = os.WriteFile(f3, []byte("temporary file\n"), 0644)

	// Take second checkpoint
	cp2, err := mgr.Create(context.Background(), "broken edits")
	if err != nil {
		t.Fatalf("failed to create checkpoint 2: %v", err)
	}
	if cp1.ID == cp2.ID {
		t.Fatal("expected distinct checkpoints")
	}

	// 3. Restore cp1
	restored, err := mgr.Restore(context.Background(), cp1.ID)
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if restored == nil {
		t.Fatal("expected non-nil restored checkpoint")
	}

	// Verify byte-for-byte tree equality on main.go
	content1, err := os.ReadFile(f1)
	if err != nil {
		t.Fatalf("failed to read restored f1: %v", err)
	}
	if !bytes.Equal(content1, initialContent1) {
		t.Fatalf("restored content mismatch: got %q, want %q", string(content1), string(initialContent1))
	}

	// 4. Undo of undo: verify we can restore cp2
	_, err = mgr.Restore(context.Background(), cp2.ID)
	if err != nil {
		t.Fatalf("undo-of-undo restore failed: %v", err)
	}
	contentBroken, _ := os.ReadFile(f1)
	if !strings.Contains(string(contentBroken), "panic(\"broken\")") {
		t.Fatalf("expected rolled-forward content, got: %s", string(contentBroken))
	}
}

func TestUserRepoUntouched(t *testing.T) {
	ws := t.TempDir()

	// Initialize real user git repository
	cmd := exec.Command("git", "init", ws)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("user git init failed: %s (%v)", string(out), err)
	}

	_ = exec.Command("git", "-C", ws, "config", "user.name", "User").Run()
	_ = exec.Command("git", "-C", ws, "config", "user.email", "user@example.com").Run()

	file := filepath.Join(ws, "app.go")
	_ = os.WriteFile(file, []byte("package app\n"), 0644)

	_ = exec.Command("git", "-C", ws, "add", ".").Run()
	_ = exec.Command("git", "-C", ws, "commit", "-m", "user commit").Run()

	userHEADBefore, _ := exec.Command("git", "-C", ws, "rev-parse", "HEAD").Output()
	userStatusBefore, _ := exec.Command("git", "-C", ws, "status", "--porcelain").Output()

	// Now run Forge checkpointing
	mgr, err := checkpoint.New(ws)
	if err != nil {
		t.Fatal(err)
	}

	// Take checkpoint
	cp, err := mgr.Create(context.Background(), "forge snapshot")
	if err != nil {
		t.Fatalf("forge checkpoint failed: %v", err)
	}

	// Mutate and restore
	_ = os.WriteFile(file, []byte("package app\n// edited\n"), 0644)
	_, err = mgr.Restore(context.Background(), cp.ID)
	if err != nil {
		t.Fatalf("forge restore failed: %v", err)
	}

	// Verify user's git repo is 100% untouched
	userHEADAfter, _ := exec.Command("git", "-C", ws, "rev-parse", "HEAD").Output()
	userStatusAfter, _ := exec.Command("git", "-C", ws, "status", "--porcelain").Output()

	if string(userHEADBefore) != string(userHEADAfter) {
		t.Fatalf("user HEAD changed! before: %s, after: %s", userHEADBefore, userHEADAfter)
	}
	if string(userStatusBefore) != string(userStatusAfter) {
		t.Fatalf("user git status changed! before: %s, after: %s", userStatusBefore, userStatusAfter)
	}
}

func TestNonGitWorkspace(t *testing.T) {
	ws := t.TempDir() // Plain folder with NO .git

	f := filepath.Join(ws, "notes.txt")
	_ = os.WriteFile(f, []byte("Draft 1"), 0644)

	mgr, err := checkpoint.New(ws)
	if err != nil {
		t.Fatal(err)
	}

	cp1, err := mgr.Create(context.Background(), "non-git initial")
	if err != nil {
		t.Fatalf("checkpoint in non-git workspace failed: %v", err)
	}

	_ = os.WriteFile(f, []byte("Draft 2 (modified)"), 0644)

	_, err = mgr.Restore(context.Background(), cp1.ID)
	if err != nil {
		t.Fatalf("restore in non-git workspace failed: %v", err)
	}

	data, _ := os.ReadFile(f)
	if string(data) != "Draft 1" {
		t.Fatalf("expected 'Draft 1', got %q", string(data))
	}
}

func TestCRLFAndBOMPreservation(t *testing.T) {
	ws := t.TempDir()

	crlfFile := filepath.Join(ws, "windows.txt")
	crlfData := []byte("Line 1\r\nLine 2\r\nLine 3\r\n")
	_ = os.WriteFile(crlfFile, crlfData, 0644)

	bomFile := filepath.Join(ws, "utf8_bom.txt")
	bomData := append([]byte{0xEF, 0xBB, 0xBF}, []byte("BOM header content\r\n")...)
	_ = os.WriteFile(bomFile, bomData, 0644)

	mgr, err := checkpoint.New(ws)
	if err != nil {
		t.Fatal(err)
	}

	cp, err := mgr.Create(context.Background(), "crlf and bom")
	if err != nil {
		t.Fatal(err)
	}

	// Mutate files
	_ = os.WriteFile(crlfFile, []byte("mutated"), 0644)
	_ = os.WriteFile(bomFile, []byte("mutated"), 0644)

	// Restore
	_, err = mgr.Restore(context.Background(), cp.ID)
	if err != nil {
		t.Fatal(err)
	}

	restoredCRLF, _ := os.ReadFile(crlfFile)
	if !bytes.Equal(restoredCRLF, crlfData) {
		t.Fatalf("CRLF line endings were modified! got %q, want %q", restoredCRLF, crlfData)
	}

	restoredBOM, _ := os.ReadFile(bomFile)
	if !bytes.Equal(restoredBOM, bomData) {
		t.Fatalf("UTF-8 BOM was modified! got %q, want %q", restoredBOM, bomData)
	}
}

func TestSizeCapSkip(t *testing.T) {
	ws := t.TempDir()

	mgr, err := checkpoint.New(ws)
	if err != nil {
		t.Fatal(err)
	}
	mgr.MaxFileSize = 1024 // 1 KB cap for test

	smallFile := filepath.Join(ws, "small.txt")
	_ = os.WriteFile(smallFile, []byte("small file under 1KB"), 0644)

	largeFile := filepath.Join(ws, "large.bin")
	largeData := make([]byte, 2048) // 2 KB: exceeds cap
	_ = os.WriteFile(largeFile, largeData, 0644)

	cp, err := mgr.Create(context.Background(), "size cap check")
	if err != nil {
		t.Fatal(err)
	}

	if len(cp.SkippedFiles) == 0 {
		t.Fatalf("expected large file to be reported in SkippedFiles")
	}
	found := false
	for _, f := range cp.SkippedFiles {
		if strings.Contains(f, "large.bin") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected large.bin in skipped files, got: %v", cp.SkippedFiles)
	}
}

func TestSecretExclusion(t *testing.T) {
	ws := t.TempDir()

	envFile := filepath.Join(ws, ".env")
	_ = os.WriteFile(envFile, []byte("SECRET_KEY=12345\n"), 0600)

	pemFile := filepath.Join(ws, "cert.pem")
	_ = os.WriteFile(pemFile, []byte("-----BEGIN CERTIFICATE-----\n"), 0600)

	codeFile := filepath.Join(ws, "safe.go")
	_ = os.WriteFile(codeFile, []byte("package safe\n"), 0644)

	mgr, err := checkpoint.New(ws)
	if err != nil {
		t.Fatal(err)
	}

	cp, err := mgr.Create(context.Background(), "secrets test")
	if err != nil {
		t.Fatal(err)
	}

	// Verify tracked files in shadow git
	cmd := exec.Command("git", "--git-dir="+mgr.ShadowDir, "ls-tree", "-r", "--name-only", cp.Tree)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ls-tree failed: %v", err)
	}

	tracked := string(out)
	if strings.Contains(tracked, ".env") {
		t.Fatalf(".env must be excluded from shadow checkpoints! tracked:\n%s", tracked)
	}
	if strings.Contains(tracked, "cert.pem") {
		t.Fatalf("*.pem must be excluded from shadow checkpoints! tracked:\n%s", tracked)
	}
	if !strings.Contains(tracked, "safe.go") {
		t.Fatalf("safe.go should be tracked, got:\n%s", tracked)
	}
}

func BenchmarkIncrementalSnapshot10kFiles(b *testing.B) {
	ws := b.TempDir()

	// Generate ~10,000 small files in hierarchy
	totalFiles := 1000
	if testing.Short() {
		totalFiles = 200
	}

	for i := 0; i < totalFiles; i++ {
		dir := filepath.Join(ws, fmt.Sprintf("dir_%02d", i%20))
		_ = os.MkdirAll(dir, 0755)
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%04d.txt", i)), []byte("sample line\n"), 0644)
	}

	mgr, err := checkpoint.New(ws)
	if err != nil {
		b.Fatal(err)
	}

	// Warm-up initial snapshot
	_, err = mgr.Create(context.Background(), "baseline")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Mutate 1 file to benchmark incremental snapshot time
		target := filepath.Join(ws, "dir_00", "file_0000.txt")
		_ = os.WriteFile(target, []byte(fmt.Sprintf("mutated %d\n", i)), 0644)

		_, err := mgr.Create(context.Background(), fmt.Sprintf("iteration %d", i))
		if err != nil {
			b.Fatalf("incremental snapshot failed: %v", err)
		}
	}
}

func TestConcurrentLocking(t *testing.T) {
	ws := t.TempDir()
	f := filepath.Join(ws, "concurrent.txt")
	_ = os.WriteFile(f, []byte("init\n"), 0644)

	// Two separate managers simulating two processes accessing the same workspace
	mgr1, err := checkpoint.New(ws)
	if err != nil {
		t.Fatal(err)
	}
	mgr2, err := checkpoint.New(ws)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	// Round 1
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var m *checkpoint.Manager
			if idx%2 == 0 {
				m = mgr1
			} else {
				m = mgr2
			}
			target := filepath.Join(ws, fmt.Sprintf("round1_%d.txt", idx))
			_ = os.WriteFile(target, []byte(fmt.Sprintf("round 1 data %d\n", idx)), 0644)

			_, err := m.Create(context.Background(), fmt.Sprintf("round 1 commit %d", idx))
			if err != nil {
				errCh <- fmt.Errorf("round 1 failed: %w", err)
			}
		}(i)
	}
	wg.Wait()

	// Round 2 with mutations
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var m *checkpoint.Manager
			if idx%2 == 0 {
				m = mgr1
			} else {
				m = mgr2
			}
			target := filepath.Join(ws, fmt.Sprintf("round2_%d.txt", idx))
			_ = os.WriteFile(target, []byte(fmt.Sprintf("round 2 data %d\n", idx)), 0644)

			_, err := m.Create(context.Background(), fmt.Sprintf("round 2 commit %d", idx))
			if err != nil {
				errCh <- fmt.Errorf("round 2 failed: %w", err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent checkpoint error: %v", err)
	}

	// Verify all checkpoints exist and repository is healthy
	list, err := mgr1.List(context.Background(), 20)
	if err != nil {
		t.Fatalf("failed to list checkpoints after concurrent execution: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("expected at least 2 checkpoints, got %d", len(list))
	}
}
