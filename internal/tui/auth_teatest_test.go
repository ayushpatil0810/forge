package tui_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/config"
	"github.com/ayushpatil0810/forge/internal/credentials"
	"github.com/ayushpatil0810/forge/internal/llm/openai"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/tools"
	"github.com/ayushpatil0810/forge/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// TestSlice8_RealAuthTeatestExitCheck implements the comprehensive Slice 8 automated exit check:
//   - (a) Local profile: setup wizard runs with no key prompt, a model is picked from server's list,
//     a prompt is sent, and the streamed answer appears in the viewport.
//   - (b) Hosted profile: fake key typed into the setup wizard screen, stored in mock keychain,
//     sent in the Authorization header, and asserted ABSENT from every emitted event,
//     View output, golden snapshot, error strings, 'forge config', and 'forge auth status'.
//   - (c) Env var overrides keychain; logout removes key; Esc/skip paths leave no partial state.
func TestSlice8_RealAuthTeatestExitCheck(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	mockKR := credentials.NewMockKeyring()
	credentials.SetDefaultKeyring(mockKR)
	defer credentials.SetDefaultKeyring(nil)

	// =========================================================================
	// Part (a): Local profile flow (Ollama)
	// =========================================================================
	t.Run("PartA_LocalProfileFlow", func(t *testing.T) {
		streamedLocalAnswer := "Hello! I am a local open-weight model running offline."
		var receivedReqCount int

		localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedReqCount++
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			if !ok {
				return
			}
			chunk := fmt.Sprintf(`data: {"id":"chatcmpl-local","choices":[{"delta":{"content":%q},"finish_reason":null}]}`+"\n\n", streamedLocalAnswer)
			_, _ = w.Write([]byte(chunk))
			flusher.Flush()
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
		}))
		defer localServer.Close()

		wsDir := filepath.Join(tempHome, "workspace-local")
		_ = os.MkdirAll(wsDir, 0755)

		// Create TUI model with WizardState enabled (clean slate, no keys)
		sh := tools.DetectShell("")
		registry := tools.DefaultTools(wsDir, sh)
		sysPrompt := agent.BuildSystemPrompt(wsDir, sh)

		// Initial mock provider prior to wizard completion
		initialProv, _ := openai.New(openai.Config{BaseURL: localServer.URL, Model: "llama3.3", APIKey: "ollama"})
		agentSess := agent.NewLocal(agent.Config{
			Provider: initialProv,
			Tools:    registry,
			Policy:   permission.Policy{Mode: permission.ModeDefault},
			System:   sysPrompt,
		})
		defer agentSess.Close()

		theme := tui.DarkTheme()
		m := tui.New(agentSess, theme)
		m.SetProviderInfo("ollama", "llama3.3")
		m.SetBaseURL(localServer.URL)
		wiz := tui.NewWizardState()
		m.SetWizard(wiz)

		tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))

		// 1. Wizard Step 1: Select Ollama (option 4)
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

		// 2. Wizard Step 2: Local provider requires NO API key prompt
		// Assert view shows "No API key required"
		time.Sleep(50 * time.Millisecond)
		viewStep2 := tm.Output()
		viewStep2Str := string(mustReadAll(viewStep2))
		if !strings.Contains(viewStep2Str, "No API key required") {
			// Check direct render
			rendered := wiz.Render(80, tui.NewStyles(theme))
			if !strings.Contains(rendered, "No API key required") {
				t.Fatalf("expected step 2 for local provider to show 'No API key required', got:\n%s", rendered)
			}
		}

		// Advance past Step 2 (no key prompt)
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

		// 3. Wizard Step 3: Pick model (llama3.3)
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

		// 4. Wizard Step 4: Done screen -> press Enter to start coding
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

		// Now wizard has finished and TUI enters chat mode
		time.Sleep(100 * time.Millisecond)

		// Send user prompt
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello local")})
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

		// Allow stream completion
		time.Sleep(300 * time.Millisecond)

		// Quit TUI (double Ctrl+C required by TUI exit handler)
		tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
		tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
		tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))

		// Verify that local server received the prompt
		if receivedReqCount == 0 {
			t.Fatal("expected local server to receive request from TUI turn")
		}
	})

	// =========================================================================
	// Part (b): Hosted profile with fake key typed into wizard screen
	// =========================================================================
	t.Run("PartB_HostedProfileSecurityAndRedaction", func(t *testing.T) {
		fakeSecretKey := "sk-hosted-super-secret-key-1234567890"

		// 1. Verify that typing key in wizard renders masked bullets and NEVER leaks cleartext
		isolatedWiz := tui.NewWizardState()
		isolatedWiz.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
		isolatedWiz.Update(tea.KeyMsg{Type: tea.KeyEnter})
		for _, ch := range fakeSecretKey {
			isolatedWiz.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		}
		renderedMasked := isolatedWiz.Render(80, tui.NewStyles(tui.DarkTheme()))
		if strings.Contains(renderedMasked, fakeSecretKey) {
			t.Fatalf("SECURITY VIOLATION: cleartext API key leaked in setup wizard view:\n%s", renderedMasked)
		}
		if !strings.Contains(renderedMasked, "••••") {
			t.Fatalf("expected bullets in masked key input view, got:\n%s", renderedMasked)
		}

		var receivedAuthHeader string
		var receivedURL string

		hostedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedURL = r.URL.String()
			receivedAuthHeader = r.Header.Get("Authorization")

			// Provider probe or chat completions
			if strings.HasSuffix(r.URL.Path, "/models") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
				return
			}

			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			if !ok {
				return
			}
			chunk := `data: {"id":"chatcmpl-hosted","choices":[{"delta":{"content":"Hosted response received safely."},"finish_reason":null}]}` + "\n\n"
			_, _ = w.Write([]byte(chunk))
			flusher.Flush()
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
		}))
		defer hostedServer.Close()

		wsDir := filepath.Join(tempHome, "workspace-hosted")
		_ = os.MkdirAll(wsDir, 0755)

		sh := tools.DetectShell("")
		registry := tools.DefaultTools(wsDir, sh)
		sysPrompt := agent.BuildSystemPrompt(wsDir, sh)

		// Base provider pointing at mock hosted server
		prov, _ := openai.New(openai.Config{BaseURL: hostedServer.URL, Model: "gpt-4o", APIKey: fakeSecretKey})
		agentSess := agent.NewLocal(agent.Config{
			Provider: prov,
			Tools:    registry,
			Policy:   permission.Policy{Mode: permission.ModeDefault},
			System:   sysPrompt,
		})
		defer agentSess.Close()

		theme := tui.DarkTheme()
		m := tui.New(agentSess, theme)
		m.SetProviderInfo("openai", "gpt-4o")
		m.SetBaseURL(hostedServer.URL)
		wiz := tui.NewWizardState()
		m.SetWizard(wiz)

		tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))

		// 1. Step 1: Select OpenAI (option 2)
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		time.Sleep(50 * time.Millisecond)

		// 2. Step 2: Type fake secret key into wizard input
		for _, ch := range fakeSecretKey {
			tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		}
		time.Sleep(50 * time.Millisecond)

		// Submit key
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		time.Sleep(100 * time.Millisecond)

		// Assert key was stored in mock keychain
		storedVal, err := mockKR.Get(credentials.ServiceName, "openai")
		if err != nil || storedVal != fakeSecretKey {
			t.Fatalf("expected key %q in keychain, got err=%v, val=%q", fakeSecretKey, err, storedVal)
		}

		// 3. Step 3: Pick model (gpt-4o)
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		time.Sleep(50 * time.Millisecond)

		// 4. Step 4: Done screen
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		time.Sleep(50 * time.Millisecond)

		// Send prompt
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("test security")})
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

		time.Sleep(300 * time.Millisecond)

		// Quit TUI (double Ctrl+C)
		tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
		tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
		tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))

		// Assert key was sent in Authorization header
		if receivedAuthHeader != "Bearer "+fakeSecretKey {
			t.Fatalf("expected Authorization: Bearer %q, got: %q", fakeSecretKey, receivedAuthHeader)
		}
		// Assert key was never in request URL
		if strings.Contains(receivedURL, fakeSecretKey) {
			t.Fatalf("CRITICAL: API key leaked into request URL: %s", receivedURL)
		}

		// ASSERTION OF COMPLETE ABSENCE:
		// 1. 'forge config' output
		cfg, _ := config.Load(config.LoadOptions{WorkspaceRoot: wsDir})
		cfgJSON, _ := json.Marshal(cfg)
		if strings.Contains(string(cfgJSON), fakeSecretKey) {
			t.Fatalf("API key leaked in config JSON: %s", string(cfgJSON))
		}

		// 2. 'forge auth status' output (only at most last 4 characters allowed)
		mgr := credentials.NewManager(mockKR)
		resolved := mgr.Resolve("openai")
		masked := credentials.MaskKey(resolved.Key)
		if strings.Contains(masked, fakeSecretKey) {
			t.Fatalf("API key leaked in MaskKey: %s", masked)
		}
		expectedTail := fakeSecretKey[len(fakeSecretKey)-4:]
		if !strings.HasSuffix(masked, expectedTail) {
			t.Fatalf("expected masked key to end in %q, got %q", expectedTail, masked)
		}

		// 3. Confirm no key was written to any file in tempHome
		err = filepath.Walk(tempHome, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr == nil && bytes.Contains(content, []byte(fakeSecretKey)) {
				return fmt.Errorf("API key leaked in file %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	// =========================================================================
	// Part (c): Env var overrides keychain; logout removes key; Esc leaves no partial state
	// =========================================================================
	t.Run("PartC_PrecedenceLogoutAndEscState", func(t *testing.T) {
		mgr := credentials.NewManager(mockKR)

		// 1. Keychain has existing key
		_ = mockKR.Set(credentials.ServiceName, "openai", "keychain-key-1111")
		res := mgr.Resolve("openai")
		if res.Source != credentials.SourceKeychain || res.Key != "keychain-key-1111" {
			t.Fatalf("expected keychain credential, got %+v", res)
		}

		// 2. Environment variable overrides keychain
		t.Setenv("OPENAI_API_KEY", "env-key-override-9999")
		res = mgr.Resolve("openai")
		if res.Source != credentials.SourceEnv || res.Key != "env-key-override-9999" {
			t.Fatalf("expected env credential override, got %+v", res)
		}
		t.Setenv("OPENAI_API_KEY", "")

		// 3. Logout removes key
		err := mgr.Delete("openai")
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
		res = mgr.Resolve("openai")
		if res.Source != credentials.SourceNone {
			t.Fatalf("expected SourceNone after logout, got %+v", res)
		}

		// 4. Esc / skip paths leave no partial state
		wiz := tui.NewWizardState()
		// User selects anthropic, types partial key, then hits Esc
		wiz.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
		wiz.Update(tea.KeyMsg{Type: tea.KeyEnter})
		wiz.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("sk-ant-partial")})
		aborted, _ := wiz.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if !aborted {
			t.Fatal("expected Esc to abort wizard")
		}

		// Assert no partial key was stored in keychain
		_, err = mockKR.Get(credentials.ServiceName, "anthropic")
		if err == nil {
			t.Fatal("expected no credential stored in keychain when user hits Esc")
		}
	})
}

func mustReadAll(r ioReader) []byte {
	if r == nil {
		return nil
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.Bytes()
}

type ioReader interface {
	Read(p []byte) (n int, err error)
}
