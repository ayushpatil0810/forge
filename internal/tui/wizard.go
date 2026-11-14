package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
	Step               wizardStep
	ProviderIndex      int
	Providers          []string
	KeyInput           string
	DetectedLocal      string
	ModelInput         string
	ContextWindowInput string
	PriceInInput       string
	PriceOutInput      string
	ActiveField        int // 0: ModelInput, 1: ContextWindowInput, 2: PriceInInput, 3: PriceOutInput
	StatusMsg          string
	// switchOnly skips the key step when a key is already stored.
	switchOnly bool
}

func NewWizardState() *WizardState {
	w := &WizardState{
		Step:          wizardStepProvider,
		ProviderIndex: 0,
		Providers:     []string{"anthropic", "openai", "gemini", "ollama", "lmstudio"},
	}
	return w
}

// NewModelSwitcher opens the provider and model list used by /models.
// A hosted provider that has no saved key still asks for one.
func NewModelSwitcher() *WizardState {
	w := NewWizardState()
	w.switchOnly = true
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
		return "Anthropic"
	case "openai":
		return "OpenAI"
	case "gemini":
		return "Google Gemini"
	case "ollama":
		return "Ollama (local server on 127.0.0.1:11434)"
	case "lmstudio":
		return "LM Studio (local server on 127.0.0.1:1234)"
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
			w.StatusMsg = ""
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

	if w.Step == wizardStepModel {
		switch keyMsg.String() {
		case "tab":
			w.ActiveField = (w.ActiveField + 1) % 4
			return false, nil
		case "shift+tab":
			w.ActiveField = (w.ActiveField + 3) % 4
			return false, nil
		case "backspace":
			switch w.ActiveField {
			case 0:
				if len(w.ModelInput) > 0 {
					w.ModelInput = w.ModelInput[:len(w.ModelInput)-1]
				}
			case 1:
				if len(w.ContextWindowInput) > 0 {
					w.ContextWindowInput = w.ContextWindowInput[:len(w.ContextWindowInput)-1]
				}
			case 2:
				if len(w.PriceInInput) > 0 {
					w.PriceInInput = w.PriceInInput[:len(w.PriceInInput)-1]
				}
			case 3:
				if len(w.PriceOutInput) > 0 {
					w.PriceOutInput = w.PriceOutInput[:len(w.PriceOutInput)-1]
				}
			}
			return false, nil
		case "enter":
			if strings.TrimSpace(w.ModelInput) == "" {
				w.StatusMsg = "Enter the model ID you want to use."
				w.ActiveField = 0
				return false, nil
			}
			w.ModelInput = strings.TrimSpace(w.ModelInput)
			w.StatusMsg = ""
			w.Step = wizardStepDone
			return false, nil
		default:
			if len(keyMsg.Runes) > 0 {
				switch w.ActiveField {
				case 0:
					w.ModelInput += string(keyMsg.Runes)
				case 1:
					w.ContextWindowInput += string(keyMsg.Runes)
				case 2:
					w.PriceInInput += string(keyMsg.Runes)
				case 3:
					w.PriceOutInput += string(keyMsg.Runes)
				}
				w.StatusMsg = ""
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
		}

	case "down", "j":
		switch w.Step {
		case wizardStepProvider:
			if w.ProviderIndex < len(w.Providers)-1 {
				w.ProviderIndex++
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
			if w.switchOnly && w.providerHasKey(p) {
				w.StatusMsg = ""
				w.Step = wizardStepModel
				break
			}
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

func (w *WizardState) providerHasKey(p string) bool {
	if p == "ollama" || p == "lmstudio" {
		return true
	}
	cred := credentials.NewManager(nil).Resolve(p)
	return cred.Source != credentials.SourceNone && strings.TrimSpace(cred.Key) != ""
}

func (w *WizardState) SelectedModel() string {
	return strings.TrimSpace(w.ModelInput)
}

func (w *WizardState) ContextWindow() int {
	cw, _ := strconv.Atoi(strings.TrimSpace(w.ContextWindowInput))
	return cw
}

func (w *WizardState) PriceIn() float64 {
	pi, _ := strconv.ParseFloat(strings.TrimSpace(w.PriceInInput), 64)
	return pi
}

func (w *WizardState) PriceOut() float64 {
	po, _ := strconv.ParseFloat(strings.TrimSpace(w.PriceOutInput), 64)
	return po
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
	heading := "Welcome to Forge"
	if w.switchOnly {
		heading = "Switch model"
	}
	step := int(w.Step)
	if step > 3 {
		step = 3
	}
	title := styles.ActivityLabel.Bold(true).Render(fmt.Sprintf(" %s (Step %d/3) ", heading, step))
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
		sb.WriteString("\n" + styles.SystemInfo.Render("No environment variable is required. The key is saved in your OS keychain."))
		sb.WriteString("\n" + styles.SystemInfo.Render("[Esc: Skip and enter the terminal]"))

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
		fmt.Fprintf(&sb, "Enter model configuration for %s:\n\n", strings.ToUpper(p))
		sb.WriteString(styles.SystemInfo.Render("used for cost display and for warning before the context fills; leave blank if unsure") + "\n\n")

		mShown := w.ModelInput
		if mShown == "" {
			mShown = "(type model ID)"
		}
		cwShown := w.ContextWindowInput
		if cwShown == "" {
			cwShown = "(optional, e.g. 128000)"
		}
		piShown := w.PriceInInput
		if piShown == "" {
			piShown = "(optional, e.g. 2.50)"
		}
		poShown := w.PriceOutInput
		if poShown == "" {
			poShown = "(optional, e.g. 10.00)"
		}

		f0, f1, f2, f3 := "  ", "  ", "  ", "  "
		switch w.ActiveField {
		case 0:
			f0 = "❯ "
		case 1:
			f1 = "❯ "
		case 2:
			f2 = "❯ "
		case 3:
			f3 = "❯ "
		}

		configBox := fmt.Sprintf("%sModel ID: %s\n%sContext window: %s\n%sInput price ($/M): %s\n%sOutput price ($/M): %s",
			f0, mShown, f1, cwShown, f2, piShown, f3, poShown)
		sb.WriteString(styles.BorderBox.Render(configBox) + "\n\n")

		if w.StatusMsg != "" {
			sb.WriteString(styles.ErrorText.Render(w.StatusMsg) + "\n\n")
		}
		sb.WriteString(styles.UserPrompt.Render("Press Enter to use this model, or Tab to edit optional fields."))

	case wizardStepDone:
		done := "✓ Setup completed successfully!"
		if w.switchOnly {
			done = "✓ Model switched."
		}
		sb.WriteString(styles.ToolCardSuccess.Render(done) + "\n\n")
		p := w.CurrentProvider()
		fmt.Fprintf(&sb, "Active Provider: %s\nModel:           %s\n", strings.ToUpper(p), w.SelectedModel())
		cw := w.ContextWindow()
		cwText := "unknown"
		if cw > 0 {
			cwText = fmt.Sprintf("%d tokens", cw)
		}
		fmt.Fprintf(&sb, "Context Window:  %s\n", cwText)
		pi := w.PriceIn()
		po := w.PriceOut()
		if pi > 0 || po > 0 {
			fmt.Fprintf(&sb, "Prices ($/M):    in $%.2f, out $%.2f\n\n", pi, po)
		} else {
			sb.WriteString("Prices ($/M):    unknown\n\n")
		}
		sb.WriteString(styles.UserPrompt.Render("• Press Enter to start coding\n• Or press 'd' to run the interactive demo\n"))
	}

	return styles.BorderBox.Width(boxWidth).Padding(1, 2).Render(sb.String())
}
