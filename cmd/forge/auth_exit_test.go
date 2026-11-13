package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/credentials"
)

func TestSlice8_AuthAndHeadlessExitCheck(t *testing.T) {
	// 1. Isolate environment
	tempDir := t.TempDir()
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
		chunk := `data: {"id":"chatcmpl-test","choices":[{"delta":{"content":"Authenticated prompt succeeded!"},"finish_reason":null}]}` + "\n\n"
		_, _ = w.Write([]byte(chunk))
		flusher.Flush()
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
	if masked != "sk-...5432" {
		t.Fatalf("expected masked key 'sk-...5432', got %q", masked)
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

	runErr := runHeadless(ctx, "Hello auth test", "text", "", 2)

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

	if !strings.Contains(outputStr, "Authenticated prompt succeeded!") {
		t.Fatalf("expected output to contain 'Authenticated prompt succeeded!', got:\n%s", outputStr)
	}
}
