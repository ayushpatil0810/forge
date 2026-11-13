package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/ayushpatil0810/forge/internal/config"
	"github.com/ayushpatil0810/forge/internal/credentials"
	tea "github.com/charmbracelet/bubbletea"
)

type wizardStep int

const (
	wizardStepProvider wizardStep = 1
	wizardStepKey      wizardStep = 2
	wizardStepModel    wizardStep = 3
	wizardStepDone     wizardStep = 4
)

type WizardState struct {
	Step          wizardStep
	ProviderIndex int
	Providers     []string
	KeyInput      string
	DetectedLocal string
	ModelIndex    int
	Models        []string
	StatusMsg     string
}

func NewWizardState() *WizardState {
	w := &WizardState{
		Step:          wizardStepProvider,
		ProviderIndex: 0,
		Providers:     []string{"anthropic", "openai", "gemini", "ollama", "lmstudio"},
	}
	return w
}

func (w *WizardState) CurrentProvider() string {
	if w.ProviderIndex >= 0 && w.ProviderIndex < len(w.Providers) {
		return w.Providers[w.ProviderIndex]
	}
	return "anthropic"
}

func (w *WizardState) ProviderDisplayName(p string) string {
	switch p {
	case "anthropic":
		return "Anthropic (Claude 3.7 / 3.5 Sonnet)"
	case "openai":
		return "OpenAI (GPT-4o, o3-mini)"
	case "gemini":
		return "Google Gemini (Gemini 2.5 Pro / Flash)"
	case "ollama":
		return "Ollama (Local open weights on 127.0.0.1:11434)"
	case "lmstudio":
		return "LM Studio (Local OpenAI server on 127.0.0.1:1234)"
	default:
		return p
	}
}

func (w *WizardState) ProviderKeyConsole(p string) string {
	switch p {
	case "anthropic":
		return "https://console.anthropic.com/settings/keys"
	case "openai":
		return "https://platform.openai.com/api-keys"
	case "gemini":
		return "https://aistudio.google.com/app/apikey"
	default:
		return ""
	}
}

func (w *WizardState) Update(msg tea.Msg) (bool, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}

	if keyMsg.String() == "esc" {
		// User aborted wizard
		return true, nil
	}

	// When typing API key in Step 2, capture all printable runes and control keys directly
	if w.Step == wizardStepKey {
		p := w.CurrentProvider()
		switch keyMsg.String() {
		case "backspace":
			if len(w.KeyInput) > 0 {
				w.KeyInput = w.KeyInput[:len(w.KeyInput)-1]
			}
			return false, nil
		case "enter":
			if p != "ollama" && p != "lmstudio" {
				if strings.TrimSpace(w.KeyInput) == "" {
					w.StatusMsg = "API key cannot be empty. Press Esc to skip."
					return false, nil
				}
				// Save key to OS keychain
				mgr := credentials.NewManager(nil)
				_, _ = mgr.Store(p, w.KeyInput, false)
			}
			// Setup models for step 3
			w.initModelsForProvider(p)
			w.Step = wizardStepModel
			return false, nil
		default:
			if p != "ollama" && p != "lmstudio" {
				runes := keyMsg.Runes
				if len(runes) > 0 {
					w.KeyInput += string(runes)
				}
			}
			return false, nil
		}
	}

	switch keyMsg.String() {
	case "up", "k":
		switch w.Step {
		case wizardStepProvider:
			if w.ProviderIndex > 0 {
				w.ProviderIndex--
			}
		case wizardStepModel:
			if w.ModelIndex > 0 {
				w.ModelIndex--
			}
		}

	case "down", "j":
		switch w.Step {
		case wizardStepProvider:
			if w.ProviderIndex < len(w.Providers)-1 {
				w.ProviderIndex++
			}
		case wizardStepModel:
			if w.ModelIndex < len(w.Models)-1 {
				w.ModelIndex++
			}
		}

	case "1", "2", "3", "4", "5":
		if w.Step == wizardStepProvider {
			idx := int(keyMsg.String()[0] - '1')
			if idx >= 0 && idx < len(w.Providers) {
				w.ProviderIndex = idx
			}
		}

	case "enter":
		switch w.Step {
		case wizardStepProvider:
			p := w.CurrentProvider()
			if p == "ollama" || p == "lmstudio" {
				ollama, lm := credentials.ProbeLocalServers(context.Background())
				if p == "ollama" && ollama {
					w.DetectedLocal = "✓ Ollama server detected on 127.0.0.1:11434"
				} else if p == "lmstudio" && lm {
					w.DetectedLocal = "✓ LM Studio server detected on 127.0.0.1:1234"
				} else {
					w.DetectedLocal = "Local server not detected on loopback. (You can start it later)."
				}
			}
			w.Step = wizardStepKey

		case wizardStepModel:
			w.Step = wizardStepDone

		case wizardStepDone:
			// Finished
			return true, nil
		}

	default:
		if w.Step == wizardStepDone {
			if keyMsg.String() == "d" || keyMsg.String() == "D" {
				// User chose to run demo
				return true, nil
			}
		}
	}

	return false, nil
}

func (w *WizardState) initModelsForProvider(p string) {
	switch p {
	case "anthropic":
		w.Models = []string{"claude-sonnet-5-5", "claude-haiku-4-5", "claude-opus-4-5"}
	case "openai":
		w.Models = []string{"gpt-4o", "gpt-4o-mini", "o3-mini", "o1"}
	case "gemini":
		w.Models = []string{"gemini-2.5-pro", "gemini-2.5-flash"}
	case "ollama":
		w.Models = []string{"llama3.3", "qwen2.5-coder:7b", "deepseek-r1:7b"}
	case "lmstudio":
		w.Models = []string{"model", "qwen2.5-coder", "deepseek-coder"}
	default:
		w.Models = []string{config.DefaultModelForProvider(p)}
	}
	w.ModelIndex = 0
}

func (w *WizardState) Render(width int, styles Styles) string {
	boxWidth := width - 6
	if boxWidth < 50 {
		boxWidth = 50
	}
	if boxWidth > 78 {
		boxWidth = 78
	}

	var sb strings.Builder
	title := styles.ActivityLabel.Bold(true).Render(fmt.Sprintf(" Welcome to Forge — Setup Wizard (Step %d/3) ", w.Step))
	sb.WriteString(title + "\n\n")

	switch w.Step {
	case wizardStepProvider:
		sb.WriteString("Select your primary LLM provider (use ↑/↓ or 1-5, Enter to continue):\n\n")
		for i, p := range w.Providers {
			prefix := "  "
			if i == w.ProviderIndex {
				prefix = "❯ "
			}
			line := fmt.Sprintf("%s%d. %s", prefix, i+1, w.ProviderDisplayName(p))
			if i == w.ProviderIndex {
				sb.WriteString(styles.UserPrompt.Render(line) + "\n")
			} else {
				sb.WriteString(styles.AssistantText.Render(line) + "\n")
			}
		}
		sb.WriteString("\n" + styles.SystemInfo.Render("[Esc: Skip setup and enter terminal directly]"))

	case wizardStepKey:
		p := w.CurrentProvider()
		if p == "ollama" || p == "lmstudio" {
			fmt.Fprintf(&sb, "Local Provider: %s\n\n", strings.ToUpper(p))
			sb.WriteString(styles.SystemInfo.Render(w.DetectedLocal) + "\n\n")
			sb.WriteString("No API key required for local models.\n")
			sb.WriteString(styles.UserPrompt.Render("Press Enter to continue.") + "\n\n")
		} else {
			fmt.Fprintf(&sb, "Enter API Key for %s:\n\n", strings.ToUpper(p))
			masked := strings.Repeat("•", len(w.KeyInput))
			if masked == "" {
				masked = "(paste key here)"
			}
			sb.WriteString(styles.BorderBox.Render("Key: "+masked) + "\n\n")
			console := w.ProviderKeyConsole(p)
			if console != "" {
				sb.WriteString(styles.SystemInfo.Render("Get your API key at: "+console) + "\n")
			}
			sb.WriteString(styles.SystemInfo.Render("Keys are stored securely in your OS Keychain and never logged.") + "\n\n")
			if w.StatusMsg != "" {
				sb.WriteString(styles.ErrorText.Render(w.StatusMsg) + "\n\n")
			}
			sb.WriteString(styles.UserPrompt.Render("Press Enter to save to keychain, or Esc to skip."))
		}

	case wizardStepModel:
		p := w.CurrentProvider()
		fmt.Fprintf(&sb, "Choose default model for %s:\n\n", strings.ToUpper(p))
		for i, m := range w.Models {
			prefix := "  "
			if i == w.ModelIndex {
				prefix = "❯ "
			}
			line := prefix + m
			if i == 0 {
				line += " (Recommended)"
			}
			if i == w.ModelIndex {
				sb.WriteString(styles.UserPrompt.Render(line) + "\n")
			} else {
				sb.WriteString(styles.AssistantText.Render(line) + "\n")
			}
		}
		sb.WriteString("\n" + styles.UserPrompt.Render("Press Enter to confirm."))

	case wizardStepDone:
		sb.WriteString(styles.ToolCardSuccess.Render("✓ Setup completed successfully!") + "\n\n")
		p := w.CurrentProvider()
		m := ""
		if w.ModelIndex < len(w.Models) {
			m = w.Models[w.ModelIndex]
		}
		fmt.Fprintf(&sb, "Active Provider: %s\nDefault Model:   %s\n\n", strings.ToUpper(p), m)
		sb.WriteString(styles.UserPrompt.Render("• Press Enter to start coding\n• Or press 'd' to run the interactive demo\n"))
	}

	return styles.BorderBox.Width(boxWidth).Padding(1, 2).Render(sb.String())
}
