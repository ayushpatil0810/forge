package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/credentials"
	"github.com/ayushpatil0810/forge/internal/session"
)

func TestConfigAndAuthStatusHideCredentialValues(t *testing.T) {
	t.Cleanup(resetCLIFlags)
	t.Setenv("OPENAI_API_KEY", "forge-fixture \"credential\" tail314")
	for _, args := range [][]string{
		{"config", "--system-prompt", "diagnostic " + os.Getenv("OPENAI_API_KEY")},
		{"auth", "status"},
	} {
		var output bytes.Buffer
		cmd := newRootCmd()
		cmd.SetOut(&output)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "tail314") || strings.Contains(output.String(), "l314") {
			t.Fatalf("%s exposed credential characters", args[0])
		}
		if args[0] == "config" && (!json.Valid(output.Bytes()) || !strings.Contains(output.String(), "[REDACTED:api_key]")) {
			t.Fatal("config should print valid JSON with the credential redacted")
		}
		if args[0] == "auth" && !strings.Contains(output.String(), "****") {
			t.Fatal("auth status should display the credential marker")
		}
	}
}

func TestSlice8_AuthAndHeadlessExitCheck(t *testing.T) {
	t.Cleanup(resetCLIFlags)
	// 1. Isolate environment
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "sessions.db")
	t.Setenv("FORGE_SESSIONS_PATH", dbPath)
	t.Setenv("HOME", tempDir)
	t.Setenv("USERPROFILE", tempDir)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	// 2. Setup mock server that requires the specific API key
	expectedKey := "sk-live-test-key-5432"
	var receivedAuthHeader string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		if receivedAuthHeader != "Bearer "+expectedKey {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_key"}`))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		for _, text := range []string{"Authenticated prompt succeeded! " + expectedKey[:10], expectedKey[10:]} {
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-test\",\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", text)
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	// 3. Store credentials using credentials.Manager into isolated mock keyring
	mockKR := credentials.NewMockKeyring()
	credentials.SetDefaultKeyring(mockKR)
	defer credentials.SetDefaultKeyring(nil)

	mgr := credentials.NewManager(mockKR)
	source, err := mgr.Store("openai", expectedKey, false)
	if err != nil {
		t.Fatalf("failed to store credential: %v", err)
	}
	if source != credentials.SourceKeychain {
		t.Fatalf("expected store to keychain, got %v", source)
	}

	// 4. Test auth status
	resolved := mgr.Resolve("openai")
	if resolved.Key != expectedKey {
		t.Fatalf("expected resolved key %q, got %q", expectedKey, resolved.Key)
	}
	masked := credentials.MaskKey(resolved.Key)
	if masked != "****" {
		t.Fatal("auth status key marker must contain no key characters")
	}

	// 5. Run headless prompt against the mock server using credentials resolved from storage
	wsDir := filepath.Join(tempDir, "workspace")
	_ = os.MkdirAll(wsDir, 0755)

	rootFlag = wsDir
	providerFlag = "openai"
	baseURLFlag = srv.URL
	modelFlag = "gpt-4o"

	// Capture stdout
	oldStdout := os.Stdout
	rPipe, wPipe, _ := os.Pipe()
	os.Stdout = wPipe

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	prefix := strings.Repeat("a", 51) + " "
	runErr := runHeadless(ctx, prefix+expectedKey, "text", "", 2)

	_ = wPipe.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(rPipe)
	outputStr := buf.String()

	if runErr != nil {
		t.Fatalf("runHeadless failed: %v", runErr)
	}

	if receivedAuthHeader != "Bearer "+expectedKey {
		t.Fatalf("server received auth header %q, expected 'Bearer %s'", receivedAuthHeader, expectedKey)
	}

	const safeResponse = "Authenticated prompt succeeded! [REDACTED:secret]"
	if strings.TrimSpace(outputStr) != safeResponse {
		t.Fatal("headless text did not redact the split provider credential")
	}
	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	rows, err := store.ListSessions(ctx, session.CanonicalWorkspace(wsDir), false)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected a persisted session, got %d (%v)", len(rows), err)
	}
	safePrompt := prefix + "[REDACTED:secret]"
	if rows[0].Title != session.TruncateTitle(safePrompt) {
		t.Fatal("session title was truncated before redaction")
	}
	messages, err := store.GetMessages(ctx, rows[0].ID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("expected a persisted conversation, got %d messages (%v)", len(messages), err)
	}
	if messages[0].Content != safePrompt || messages[1].Content != safeResponse {
		t.Fatal("persisted conversation did not redact both messages")
	}
}
