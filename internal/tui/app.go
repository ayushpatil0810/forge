package tui

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/config"
	"github.com/ayushpatil0810/forge/internal/credentials"
	"github.com/ayushpatil0810/forge/internal/llm"
	"github.com/ayushpatil0810/forge/internal/permission"
	"github.com/ayushpatil0810/forge/internal/session"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes ANSI escape sequences from a string.
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func isDumbTerminal() bool {
	return os.Getenv("TERM") == "dumb" || os.Getenv("NO_COLOR") != ""
}

func truncateVisual(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	clean := StripANSI(s)
	if runewidth.StringWidth(clean) <= maxWidth {
		return s
	}
	var curWidth int
	var truncated []rune
	for _, r := range clean {
		rw := runewidth.RuneWidth(r)
		if curWidth+rw > maxWidth-3 {
			break
		}
		truncated = append(truncated, r)
		curWidth += rw
	}
	return string(truncated) + "..."
}

type sessionClosedMsg struct{}
type renderTickMsg struct{}
type resetCtrlCMsg struct{}

type messageItem interface {
	render(width int, expanded bool, styles Styles) string
}

type userMessage struct {
	text string
}

func (u userMessage) render(_ int, _ bool, styles Styles) string {
	prompt := "❯ "
	if isDumbTerminal() {
		prompt = "> "
	}
	return styles.UserPrompt.Render(prompt + u.text)
}

type assistantMessage struct {
	rendered string
}

func (a assistantMessage) render(_ int, _ bool, _ Styles) string {
	return a.rendered
}

type toolMessage struct {
	card *ToolCard
}

func (t toolMessage) render(width int, expanded bool, styles Styles) string {
	return t.card.Render(width, expanded, styles)
}

type errorMessage struct {
	text string
}

func (e errorMessage) render(_ int, _ bool, styles Styles) string {
	return styles.ErrorText.Render(e.text)
}

type systemMessage struct {
	text string
}

func (s systemMessage) render(_ int, _ bool, styles Styles) string {
	return styles.SystemInfo.Render(s.text)
}

type errorCard struct {
	code     string
	message  string
	guidance string
}

func (e errorCard) render(width int, _ bool, styles Styles) string {
	boxWidth := width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}
	icon := "✕"
	if isDumbTerminal() {
		icon = "[X]"
	}
	title := styles.ToolCardError.Bold(true).Render(fmt.Sprintf("%s ERROR [%s]", icon, e.code))
	msg := styles.ErrorText.Render(e.message)
	content := title + "\n" + msg
	if e.guidance != "" {
		content += "\n\n" + styles.SystemInfo.Render("[i] How to fix: "+e.guidance)
	}
	return styles.ToolCardError.Width(boxWidth).Padding(0, 1).Render(content)
}

// Model is the Bubble Tea TUI state.
type Model struct {
	session  agent.Session
	theme    Theme
	styles   Styles
	renderer *MarkdownRenderer

	viewport viewport.Model
	textarea textarea.Model
	spinner  spinner.Model

	width  int
	height int
	ready  bool

	items           []messageItem
	toolCards       map[string]*ToolCard
	toolsExpanded   bool
	currentResponse string
	activityLabel   string
	isStreaming     bool
	renderPending   bool
	inputTokens     int
	outputTokens    int
	cost            float64
	providerName    string
	modelName       string
	baseURL         string
	version         string
	branch          string
	workspaceRoot   string
	store           *session.Store
	noSession       bool

	pendingApproval  *agent.ApprovalRequestedPayload
	showApprovalDiff bool
	ctrlCPressed     bool
	wizard           *WizardState
	sessionPicker    *SessionPicker
}

// New creates a new TUI model bound to a Session.
func New(s agent.Session, theme Theme) Model {
	ta := textarea.New()
	ta.Placeholder = "Ask Forge a question or describe a change... (Enter to send, Alt+Enter for newline)"
	ta.Focus()
	sp := spinner.New()
	if isDumbTerminal() {
		ta.Prompt = "> "
		sp.Spinner = spinner.Line
	} else {
		ta.Prompt = "❯ "
		sp.Spinner = spinner.Dot
	}
	ta.CharLimit = 16384
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	sp.Style = NewStyles(theme).ActivityLabel

	m := Model{
		session:       s,
		theme:         theme,
		styles:        NewStyles(theme),
		renderer:      NewMarkdownRenderer(theme.GlamourStyle, 80),
		textarea:      ta,
		spinner:       sp,
		activityLabel: "Ready",
		toolCards:     make(map[string]*ToolCard),
	}
	return m
}

func (m *Model) SetProviderInfo(provider, model string) {
	m.providerName = provider
	m.modelName = model
}

func (m *Model) SetBaseURL(url string) {
	m.baseURL = url
}

func (m *Model) SetWizard(w *WizardState) {
	m.wizard = w
}

func (m *Model) SetWorkspaceRoot(root string) {
	m.workspaceRoot = root
}

func (m *Model) SetSessionStore(s *session.Store, noSession bool) {
	m.store = s
	m.noSession = noSession
}

func (m *Model) SetSessionPicker(p *SessionPicker) {
	m.sessionPicker = p
}

func (m *Model) RestoreSession(data *session.ResumedSessionData) {
	m.items = nil
	m.toolCards = make(map[string]*ToolCard)
	m.cost = data.TotalCost
	m.inputTokens = data.TotalTokens / 2
	m.outputTokens = data.TotalTokens / 2
	m.providerName = data.Session.Provider
	m.modelName = data.Session.Model

	for _, msg := range data.Messages {
		switch msg.Role {
		case "user":
			m.items = append(m.items, userMessage{text: msg.Content})
		case "assistant":
			if msg.Content != "" {
				rendered, _ := m.renderer.Render(msg.Content)
				m.items = append(m.items, assistantMessage{rendered: rendered})
			}
			if tcs, ok := data.ToolCallsByMsg[msg.ID]; ok {
				for _, tc := range tcs {
					card := NewToolCard(tc.ID, tc.Name, tc.Input)
					card.Status = agent.ToolOK
					if tc.Status == "error" || tc.Status == "interrupted" {
						card.Status = agent.ToolError
					}
					card.Output = tc.Output
					m.items = append(m.items, toolMessage{card: card})
					m.toolCards[tc.ID] = card
				}
			}
		}
	}

	note := session.ResumeSystemNote(data.Session.ID)
	m.items = append(m.items, systemMessage{text: note})
	m.updateViewportContent()
}

func (m *Model) SetVersion(v string) {
	m.version = v
	m.updateViewportContent()
}

func (m *Model) SetGitBranch(b string) {
	m.branch = b
	m.updateViewportContent()
}

func (m *Model) cyclePermissionMode() {
	curr := m.session.Mode()
	var next permission.Mode
	switch curr {
	case permission.ModeDefault: // "ask"
		next = permission.ModeAcceptEdits
	case permission.ModeAcceptEdits:
		next = permission.ModePlan
	case permission.ModePlan:
		next = permission.ModeDefault
	default:
		next = permission.ModeDefault
	}
	m.session.SetMode(next)
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.spinner.Tick,
		waitForEvent(m.session.Events()),
	)
}

func waitForEvent(ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return sessionClosedMsg{}
		}
		return ev
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.wizard != nil {
		switch msg := msg.(type) {
		case tea.WindowSizeMsg:
			m.width = msg.Width
			m.height = msg.Height
			return m, nil
		case tea.KeyMsg:
			done, cmd := m.wizard.Update(msg)
			if done {
				if m.wizard.Step == wizardStepDone {
					m.providerName = m.wizard.CurrentProvider()
					if m.wizard.ModelIndex < len(m.wizard.Models) {
						m.modelName = m.wizard.Models[m.wizard.ModelIndex]
					}
					credMgr := credentials.NewManager(nil)
					cred := credMgr.Resolve(m.providerName)
					if p, err := llm.NewProvider(llm.ProviderConfig{
						Provider: m.providerName,
						Model:    m.modelName,
						BaseURL:  m.baseURL,
						APIKey:   cred.Key,
					}); err == nil {
						m.session.SetProvider(p)
					}
				}
				m.wizard = nil
				m.textarea.Focus()
				m.updateViewportContent()
				return m, cmd
			}
			return m, cmd
		}
	}

	if m.sessionPicker != nil {
		switch msg := msg.(type) {
		case tea.WindowSizeMsg:
			m.width = msg.Width
			m.height = msg.Height
			return m, nil
		case tea.KeyMsg:
			selectedID, done, cmd := m.sessionPicker.Update(msg)
			if done {
				m.sessionPicker = nil
				if selectedID != "" {
					store, err := session.NewStore("")
					if err == nil {
						resumed, err := session.LoadResumedSession(context.Background(), store, selectedID)
						if err == nil {
							m.RestoreSession(resumed)
							credMgr := credentials.NewManager(nil)
							cred := credMgr.Resolve(resumed.Session.Provider)
							if p, err := llm.NewProvider(llm.ProviderConfig{
								Provider: resumed.Session.Provider,
								Model:    resumed.Session.Model,
								BaseURL:  m.baseURL,
								APIKey:   cred.Key,
							}); err == nil {
								m.session.SetProvider(p)
							}
						} else {
							m.items = append(m.items, errorMessage{text: fmt.Sprintf("Failed to resume session: %v", err)})
						}
					}
				}
				m.textarea.Focus()
				m.updateViewportContent()
				return m, cmd
			}
			return m, cmd
		}
	}

	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
		spCmd tea.Cmd
		cmds  []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.renderer.SetWidth(msg.Width - 4)

		headerHeight := 1
		statusBarHeight := 1
		inputHeight := 5
		viewportHeight := msg.Height - headerHeight - statusBarHeight - inputHeight
		if viewportHeight < 3 {
			viewportHeight = 3
		}

		if !m.ready {
			m.viewport = viewport.New(msg.Width, viewportHeight)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = viewportHeight
		}

		m.textarea.SetWidth(msg.Width - 2)
		m.updateViewportContent()

	case tea.KeyMsg:
		if m.pendingApproval != nil {
			switch msg.String() {
			case "y", "Y":
				_ = m.session.Respond(m.pendingApproval.ID, agent.DecisionAllow)
				m.pendingApproval = nil
				m.textarea.Focus()
				m.updateViewportContent()
				return m, nil
			case "a", "A":
				if !m.pendingApproval.IsDangerous {
					_ = m.session.Respond(m.pendingApproval.ID, agent.DecisionAllowAlways)
					m.pendingApproval = nil
					m.textarea.Focus()
					m.updateViewportContent()
					return m, nil
				}
				return m, nil
			case "n", "N":
				_ = m.session.Respond(m.pendingApproval.ID, agent.DecisionDeny)
				m.pendingApproval = nil
				m.textarea.Focus()
				m.updateViewportContent()
				return m, nil
			case "e", "E":
				m.showApprovalDiff = !m.showApprovalDiff
				m.updateViewportContent()
				return m, nil
			case "tab":
				m.cyclePermissionMode()
				m.updateViewportContent()
				return m, nil
			case "ctrl+c", "esc":
				_ = m.session.Respond(m.pendingApproval.ID, agent.DecisionDeny)
				m.pendingApproval = nil
				m.textarea.Focus()
				m.updateViewportContent()
				return m, nil
			default:
				return m, nil
			}
		}

		if msg.Type == tea.KeyTab && strings.TrimSpace(m.textarea.Value()) == "" {
			m.cyclePermissionMode()
			m.updateViewportContent()
			return m, nil
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			if m.ctrlCPressed {
				m.session.Cancel()
				return m, tea.Quit
			}
			m.ctrlCPressed = true
			if m.isStreaming {
				m.session.Cancel()
			}
			cmds = append(cmds, tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg {
				return resetCtrlCMsg{}
			}))
			return m, tea.Batch(cmds...)

		case tea.KeyCtrlO:
			m.toolsExpanded = !m.toolsExpanded
			m.updateViewportContent()
			return m, nil

		case tea.KeyEsc:
			if m.isStreaming {
				m.session.Cancel()
				m.isStreaming = false
				m.activityLabel = "Interrupted"
				if m.currentResponse != "" {
					rendered, _ := m.renderer.Render(m.currentResponse + "\n\n*[Interrupted]*")
					m.items = append(m.items, assistantMessage{rendered: rendered})
					m.currentResponse = ""
					m.updateViewportContent()
				}
				return m, nil
			}

		case tea.KeyEnter:
			if msg.Alt {
				m.textarea.InsertString("\n")
				return m, nil
			}

			input := strings.TrimSpace(m.textarea.Value())
			if input == "" || m.isStreaming {
				return m, nil
			}

			if strings.HasPrefix(input, "/mode") {
				parts := strings.Fields(input)
				if len(parts) == 1 {
					m.cyclePermissionMode()
				} else {
					targetMode := permission.Mode(strings.ToLower(parts[1]))
					if targetMode == permission.ModeYOLO {
						m.items = append(m.items, errorMessage{text: "yolo mode can only be enabled at startup via --yolo"})
						m.textarea.Reset()
						m.updateViewportContent()
						return m, nil
					}
					if targetMode == permission.ModeDefault || targetMode == permission.ModeAcceptEdits || targetMode == permission.ModePlan {
						m.session.SetMode(targetMode)
					}
				}
				m.items = append(m.items, userMessage{text: input})
				m.textarea.Reset()
				m.updateViewportContent()
				return m, nil
			}

			if input == "/checkpoints" {
				m.items = append(m.items, userMessage{text: input})
				m.textarea.Reset()
				chk := m.session.Checkpointer()
				if chk == nil {
					m.items = append(m.items, systemMessage{text: "Checkpoints are not enabled for this session."})
				} else {
					list, err := chk.List(context.Background(), 10)
					if err != nil {
						m.items = append(m.items, errorMessage{text: fmt.Sprintf("failed to list checkpoints: %v", err)})
					} else if len(list) == 0 {
						m.items = append(m.items, systemMessage{text: "No checkpoints recorded yet. Snapshots are created automatically before file edits."})
					} else {
						var sb strings.Builder
						sb.WriteString("Checkpoints (newest first):\n")
						for _, cp := range list {
							fmt.Fprintf(&sb, "• [%s] %s (%s)\n", cp.ID, cp.Message, cp.Timestamp.Format("15:04:05"))
						}
						sb.WriteString("\nUse /restore <id> or /undo to revert files.")
						m.items = append(m.items, systemMessage{text: sb.String()})
					}
				}
				m.updateViewportContent()
				return m, nil
			}

			if input == "/undo" {
				m.items = append(m.items, userMessage{text: input})
				m.textarea.Reset()
				chk := m.session.Checkpointer()
				if chk == nil {
					m.items = append(m.items, systemMessage{text: "Checkpoints are not enabled for this session."})
				} else {
					list, err := chk.List(context.Background(), 2)
					if err != nil || len(list) == 0 {
						m.items = append(m.items, errorMessage{text: "No checkpoints available to undo."})
					} else {
						target := list[0]
						if len(list) > 1 {
							target = list[1] // Previous checkpoint
						}
						info, err := chk.Restore(context.Background(), target.ID)
						if err != nil {
							m.items = append(m.items, errorMessage{text: fmt.Sprintf("undo failed: %v", err)})
						} else {
							m.items = append(m.items, systemMessage{text: fmt.Sprintf("Restored workspace to checkpoint %s (%s)", info.ID, target.Message)})
						}
					}
				}
				m.updateViewportContent()
				return m, nil
			}

			if strings.HasPrefix(input, "/restore") {
				m.items = append(m.items, userMessage{text: input})
				m.textarea.Reset()
				parts := strings.Fields(input)
				if len(parts) < 2 {
					m.items = append(m.items, errorMessage{text: "Usage: /restore <checkpoint_id>"})
				} else {
					targetID := parts[1]
					chk := m.session.Checkpointer()
					if chk == nil {
						m.items = append(m.items, systemMessage{text: "Checkpoints are not enabled for this session."})
					} else {
						info, err := chk.Restore(context.Background(), targetID)
						if err != nil {
							m.items = append(m.items, errorMessage{text: fmt.Sprintf("restore failed: %v", err)})
						} else {
							m.items = append(m.items, systemMessage{text: fmt.Sprintf("Restored workspace to checkpoint %s", info.ID)})
						}
					}
				}
				m.updateViewportContent()
				return m, nil
			}

			if strings.HasPrefix(input, "/model") {
				m.items = append(m.items, userMessage{text: input})
				m.textarea.Reset()
				parts := strings.Fields(input)
				if len(parts) == 1 {
					catalog := getModelCatalog(m.providerName)
					var sb strings.Builder
					fmt.Fprintf(&sb, "Models for %s:\n", strings.ToUpper(m.providerName))
					for _, item := range catalog {
						marker := "  "
						if item.ID == m.modelName {
							marker = "• [active] "
						}
						fmt.Fprintf(&sb, "%s%-20s (context: %dk, cost: $%.2f/$%.2f)\n", marker, item.ID, item.ContextWindow/1000, item.InputCostPerM, item.OutputCostPerM)
					}
					sb.WriteString("\nUse /model <id> to switch model.")
					m.items = append(m.items, systemMessage{text: sb.String()})
				} else {
					targetModel := parts[1]
					catalog := getModelCatalog(m.providerName)
					var targetInfo *ModelCatalogItem
					for _, item := range catalog {
						if item.ID == targetModel {
							targetInfo = &item
							break
						}
					}
					approxTokens := m.inputTokens + m.outputTokens
					if targetInfo != nil && targetInfo.ContextWindow > 0 {
						if approxTokens > targetInfo.ContextWindow {
							m.items = append(m.items, errorMessage{text: fmt.Sprintf("Cannot switch to model %q: current conversation (~%d tokens) exceeds model context window (%d tokens).", targetModel, approxTokens, targetInfo.ContextWindow)})
							m.updateViewportContent()
							return m, nil
						}
						if approxTokens > targetInfo.ContextWindow*8/10 {
							m.items = append(m.items, systemMessage{text: fmt.Sprintf("Warning: current conversation (~%d tokens) is approaching model context window (%d tokens).", approxTokens, targetInfo.ContextWindow)})
						}
					}
					m.modelName = targetModel
					credMgr := credentials.NewManager(nil)
					cred := credMgr.Resolve(m.providerName)
					if p, err := llm.NewProvider(llm.ProviderConfig{
						Provider: m.providerName,
						Model:    m.modelName,
						BaseURL:  m.baseURL,
						APIKey:   cred.Key,
					}); err == nil {
						m.session.SetProvider(p)
					}
					m.items = append(m.items, systemMessage{text: fmt.Sprintf("Switched active model to %s.", targetModel)})
				}
				m.updateViewportContent()
				return m, nil
			}

			if input == "/resume" || input == "/sessions" || strings.HasPrefix(input, "/resume ") {
				m.items = append(m.items, userMessage{text: input})
				m.textarea.Reset()
				parts := strings.Fields(input)
				store, err := session.NewStore("")
				if err != nil {
					m.items = append(m.items, errorMessage{text: fmt.Sprintf("failed to open session store: %v", err)})
					m.updateViewportContent()
					return m, nil
				}
				if len(parts) > 1 {
					targetID := parts[1]
					resumed, err := session.LoadResumedSession(context.Background(), store, targetID)
					if err != nil {
						m.items = append(m.items, errorMessage{text: fmt.Sprintf("failed to resume session %q: %v", targetID, err)})
					} else {
						m.RestoreSession(resumed)
						credMgr := credentials.NewManager(nil)
						cred := credMgr.Resolve(resumed.Session.Provider)
						if p, err := llm.NewProvider(llm.ProviderConfig{
							Provider: resumed.Session.Provider,
							Model:    resumed.Session.Model,
							BaseURL:  m.baseURL,
							APIKey:   cred.Key,
						}); err == nil {
							m.session.SetProvider(p)
						}
					}
				} else {
					list, err := store.ListSessions(context.Background(), m.workspaceRoot, false)
					if err != nil {
						m.items = append(m.items, errorMessage{text: fmt.Sprintf("failed to list sessions: %v", err)})
					} else {
						m.sessionPicker = NewSessionPicker(list)
					}
				}
				m.updateViewportContent()
				return m, nil
			}

			if strings.HasPrefix(input, "/provider") {
				m.items = append(m.items, userMessage{text: input})
				m.textarea.Reset()
				parts := strings.Fields(input)
				if len(parts) == 1 {
					var sb strings.Builder
					sb.WriteString("Supported Providers:\n")
					for _, p := range []string{"anthropic", "openai", "gemini", "ollama", "lmstudio"} {
						marker := "  "
						if p == m.providerName {
							marker = "• [active] "
						}
						fmt.Fprintf(&sb, "%s%s\n", marker, p)
					}
					sb.WriteString("\nUse /provider <name> to switch provider.")
					m.items = append(m.items, systemMessage{text: sb.String()})
				} else {
					targetProvider := strings.ToLower(parts[1])
					m.providerName = targetProvider
					m.modelName = config.DefaultModelForProvider(targetProvider)
					credMgr := credentials.NewManager(nil)
					cred := credMgr.Resolve(m.providerName)
					if p, err := llm.NewProvider(llm.ProviderConfig{
						Provider: m.providerName,
						Model:    m.modelName,
						BaseURL:  m.baseURL,
						APIKey:   cred.Key,
					}); err == nil {
						m.session.SetProvider(p)
					}
					m.items = append(m.items, systemMessage{text: fmt.Sprintf("Switched active provider to %s (default model: %s).", targetProvider, m.modelName)})
				}
				m.updateViewportContent()
				return m, nil
			}

			if strings.HasPrefix(input, "/auth") {
				m.items = append(m.items, userMessage{text: input})
				m.textarea.Reset()
				mgr := credentials.NewManager(nil)
				providers := []string{"anthropic", "openai", "gemini", "ollama", "lmstudio"}
				var sb strings.Builder
				sb.WriteString("Provider Authentication Status:\n")
				for _, p := range providers {
					if p == "ollama" || p == "lmstudio" {
						fmt.Fprintf(&sb, "• %-10s: ready (local server, no key needed)\n", p)
						continue
					}
					res := mgr.Resolve(p)
					if res.Source != credentials.SourceNone && res.Key != "" {
						fmt.Fprintf(&sb, "• %-10s: authenticated (%s, masked: %s)\n", p, res.Source, credentials.MaskKey(res.Key))
					} else {
						fmt.Fprintf(&sb, "• %-10s: unconfigured (run 'forge auth login %s' in terminal)\n", p, p)
					}
				}
				m.items = append(m.items, systemMessage{text: sb.String()})
				m.updateViewportContent()
				return m, nil
			}

			m.items = append(m.items, userMessage{text: input})
			m.textarea.Reset()
			m.isStreaming = true
			if m.providerName == "ollama" || m.providerName == "lmstudio" {
				m.activityLabel = "Loading model..."
			} else {
				m.activityLabel = "Thinking..."
			}
			m.currentResponse = ""
			m.updateViewportContent()

			if m.store != nil && !m.noSession {
				msgID := fmt.Sprintf("msg_%d_u", time.Now().UnixNano())
				_ = m.store.SaveMessage(context.Background(), session.MessageRecord{
					ID:        msgID,
					SessionID: m.session.ID(),
					Role:      "user",
					Content:   input,
					CreatedAt: time.Now(),
				})
			}

			if err := m.session.Send(context.Background(), input); err != nil {
				m.isStreaming = false
				m.items = append(m.items, errorMessage{text: "Send error: " + err.Error()})
				m.updateViewportContent()
			}
			return m, nil
		}

	case resetCtrlCMsg:
		m.ctrlCPressed = false

	case renderTickMsg:
		m.renderPending = false
		m.updateViewportContent()

	case agent.Event:
		ev := msg
		switch ev.Type {
		case agent.EventTokenDelta:
			m.isStreaming = true
			if ev.Token != nil {
				m.currentResponse += ev.Token.Text
				if m.activityLabel == "Thinking..." || m.activityLabel == "Loading model..." || m.activityLabel == "Ready" {
					m.activityLabel = "Writing..."
				}
				if !m.renderPending {
					m.renderPending = true
					cmds = append(cmds, tea.Tick(40*time.Millisecond, func(time.Time) tea.Msg {
						return renderTickMsg{}
					}))
				}
			}

		case agent.EventThinkingDelta:
			m.isStreaming = true
			m.activityLabel = "Thinking..."

		case agent.EventToolStarted:
			if ev.ToolStart != nil {
				m.isStreaming = true
				// If we had buffered assistant response text, commit it before tool card
				if m.currentResponse != "" {
					rendered, _ := m.renderer.Render(m.currentResponse)
					m.items = append(m.items, assistantMessage{rendered: rendered})
					if m.store != nil && !m.noSession {
						asstMsgID := fmt.Sprintf("msg_%d_a", time.Now().UnixNano())
						_ = m.store.SaveMessage(context.Background(), session.MessageRecord{
							ID:        asstMsgID,
							SessionID: m.session.ID(),
							Role:      "assistant",
							Content:   m.currentResponse,
							CreatedAt: time.Now(),
						})
					}
					m.currentResponse = ""
				}
				tc := NewToolCard(ev.ToolStart.ID, ev.ToolStart.Name, ev.ToolStart.Input)
				m.toolCards[tc.ID] = tc
				m.items = append(m.items, toolMessage{card: tc})
				m.activityLabel = fmt.Sprintf("Running %s...", tc.Name)
				if m.store != nil && !m.noSession {
					_ = m.store.SaveToolCall(context.Background(), session.ToolCallRecord{
						ID:        ev.ToolStart.ID,
						Name:      ev.ToolStart.Name,
						Input:     ev.ToolStart.Input,
						Status:    "running",
						CreatedAt: time.Now(),
					})
				}
				m.updateViewportContent()
			}

		case agent.EventToolOutputDelta:
			if ev.ToolOut != nil {
				if tc, ok := m.toolCards[ev.ToolOut.ID]; ok {
					tc.Output += ev.ToolOut.Chunk
					m.updateViewportContent()
				}
			}

		case agent.EventToolFinished:
			if ev.ToolDone != nil {
				if tc, ok := m.toolCards[ev.ToolDone.ID]; ok {
					tc.Status = ev.ToolDone.Status
					tc.DurationMs = ev.ToolDone.DurationMs
					if ev.ToolDone.Result != "" {
						tc.Output = ev.ToolDone.Result
					}
					if ev.ToolDone.Error != "" {
						tc.Output = ev.ToolDone.Error
					}
					m.activityLabel = "Thinking..."
					if m.store != nil && !m.noSession {
						_ = m.store.SaveToolCall(context.Background(), session.ToolCallRecord{
							ID:         ev.ToolDone.ID,
							Status:     string(ev.ToolDone.Status),
							Output:     tc.Output,
							DurationMs: ev.ToolDone.DurationMs,
						})
					}
					m.updateViewportContent()
				}
			}

		case agent.EventApprovalRequested:
			if ev.Approval != nil {
				m.pendingApproval = ev.Approval
				m.showApprovalDiff = true
				m.activityLabel = "Awaiting approval..."
				m.textarea.Blur()
				m.updateViewportContent()
			}

		case agent.EventUsageUpdated:
			if ev.Usage != nil {
				m.inputTokens = ev.Usage.InputTokens
				m.outputTokens = ev.Usage.OutputTokens
				m.cost = ev.Usage.Cost
				if m.store != nil && !m.noSession {
					_ = m.store.SaveUsage(context.Background(), session.UsageRecord{
						SessionID:    m.session.ID(),
						Model:        m.modelName,
						InputTokens:  ev.Usage.InputTokens,
						OutputTokens: ev.Usage.OutputTokens,
						Cost:         ev.Usage.Cost,
						CreatedAt:    time.Now(),
					})
				}
			}

		case agent.EventDone:
			m.isStreaming = false
			m.pendingApproval = nil
			m.textarea.Focus()
			m.activityLabel = "Ready"
			if m.currentResponse != "" {
				if ev.Done != nil && ev.Done.Reason == "cancelled" {
					m.currentResponse += "\n\n*[Interrupted]*"
				}
				rendered, _ := m.renderer.Render(m.currentResponse)
				m.items = append(m.items, assistantMessage{rendered: rendered})
				if m.store != nil && !m.noSession {
					asstMsgID := fmt.Sprintf("msg_%d_a", time.Now().UnixNano())
					_ = m.store.SaveMessage(context.Background(), session.MessageRecord{
						ID:        asstMsgID,
						SessionID: m.session.ID(),
						Role:      "assistant",
						Content:   m.currentResponse,
						CreatedAt: time.Now(),
					})
				}
				m.currentResponse = ""
			}
			m.updateViewportContent()

		case agent.EventRetryScheduled:
			if ev.Retry != nil {
				m.activityLabel = fmt.Sprintf("Retrying in %dms (attempt %d/%d)...", ev.Retry.DelayMs, ev.Retry.Attempt, ev.Retry.MaxAttempts)
				m.updateViewportContent()
			}

		case agent.EventError:
			m.isStreaming = false
			m.pendingApproval = nil
			m.textarea.Focus()
			m.activityLabel = "Error"
			code := "error"
			errMsg := "unknown error"
			guidance := ""
			if ev.Err != nil {
				code = ev.Err.Code
				errMsg = ev.Err.Message
				guidance = ev.Err.Guidance
			}
			m.items = append(m.items, errorCard{code: code, message: errMsg, guidance: guidance})
			m.updateViewportContent()
		}
		// Continue listening for next session event
		cmds = append(cmds, waitForEvent(m.session.Events()))

	case sessionClosedMsg:
		// Channel closed
	}

	m.textarea, tiCmd = m.textarea.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)
	m.spinner, spCmd = m.spinner.Update(msg)
	cmds = append(cmds, tiCmd, vpCmd, spCmd)

	return m, tea.Batch(cmds...)
}

func renderWelcomeBanner(width int, version, provider, model, mode, branch string, styles Styles) string {
	boxWidth := width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}
	if width < 30 {
		boxWidth = width - 2
	}
	if boxWidth < 10 {
		boxWidth = 10
	}

	bullet := "• "
	if isDumbTerminal() {
		bullet = "* "
	}

	var title string
	if width >= 70 {
		banner := "   ___                            \n" +
			"  / __\\___  _ __ __ _  ___        \n" +
			" / _\\/ _ \\| '__/ _` |/ _ \\       \n" +
			"/ / | (_) | | | (_| |  __/       \n" +
			"\\/   \\___/|_|  \\__, |\\___|       \n" +
			"               |___/              "
		title = styles.UserPrompt.Bold(true).Render(banner)
	} else {
		title = styles.UserPrompt.Bold(true).Render("FORGE")
	}

	if version != "" {
		title += " " + styles.SystemInfo.Render("v"+version)
	}

	if provider == "" {
		provider = "anthropic"
	}
	if model == "" {
		model = "claude-sonnet-5-5"
	}

	var details []string
	details = append(details, fmt.Sprintf("%sProvider: %s", bullet, styles.AssistantText.Bold(true).Render(provider)))
	details = append(details, fmt.Sprintf("%sModel:    %s", bullet, styles.AssistantText.Bold(true).Render(model)))
	details = append(details, fmt.Sprintf("%sMode:     %s", bullet, styles.AssistantText.Bold(true).Render(mode)))
	if branch != "" {
		details = append(details, fmt.Sprintf("%sBranch:   %s", bullet, styles.AssistantText.Bold(true).Render("git:"+branch)))
	}

	var tips string
	if width < 60 {
		tips = fmt.Sprintf(
			"\n%s\n%s\n%s",
			styles.SystemInfo.Render("Tab: mode"),
			styles.SystemInfo.Render("Ctrl+O: tools"),
			styles.SystemInfo.Render("Ctrl+C: quit"),
		)
	} else {
		tips = fmt.Sprintf(
			"\n%s  %s  %s",
			styles.SystemInfo.Render("Tab: mode"),
			styles.SystemInfo.Render("Ctrl+O: tools"),
			styles.SystemInfo.Render("Ctrl+C: quit"),
		)
	}

	body := title + "\n\n" + strings.Join(details, "\n") + "\n" + tips
	return styles.BorderBox.Width(boxWidth).Padding(0, 1).Render(body)
}

func (m *Model) updateViewportContent() {
	renderWidth := m.width
	if renderWidth <= 0 {
		renderWidth = 80
	}

	if len(m.items) == 0 && m.currentResponse == "" && m.pendingApproval == nil {
		modeStr := string(m.session.Mode())
		banner := renderWelcomeBanner(renderWidth, m.version, m.providerName, m.modelName, modeStr, m.branch, m.styles)
		m.viewport.SetContent(banner)
		return
	}

	var fullText strings.Builder
	for i, item := range m.items {
		if i > 0 {
			fullText.WriteString("\n\n")
		}
		fullText.WriteString(item.render(renderWidth, m.toolsExpanded, m.styles))
	}

	if m.currentResponse != "" {
		if fullText.Len() > 0 {
			fullText.WriteString("\n\n")
		}
		rendered, _ := m.renderer.Render(m.currentResponse)
		fullText.WriteString(rendered)
	}

	if m.pendingApproval != nil {
		if fullText.Len() > 0 {
			fullText.WriteString("\n\n")
		}
		modeStr := string(m.session.Mode())
		card := RenderApprovalCard(m.pendingApproval, modeStr, m.showApprovalDiff, renderWidth, m.styles)
		fullText.WriteString(card)
	}

	wasBottom := m.viewport.AtBottom()
	m.viewport.SetContent(fullText.String())
	if wasBottom || m.isStreaming {
		m.viewport.GotoBottom()
	}
}

func (m Model) View() string {
	if m.wizard != nil {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.wizard.Render(m.width, m.styles))
	}
	if m.sessionPicker != nil {
		return RenderPickerInPlace(m.sessionPicker, m.width, m.height, m.styles)
	}

	if !m.ready {
		return "Initializing Forge TUI...\n"
	}

	sep := " · "
	if isDumbTerminal() {
		sep = " | "
	}

	var status string
	providerInfo := ""
	if m.providerName != "" {
		if m.modelName != "" {
			providerInfo = fmt.Sprintf("[%s/%s] ", m.providerName, m.modelName)
		} else {
			providerInfo = fmt.Sprintf("[%s] ", m.providerName)
		}
	}

	costInfo := ""
	if m.providerName == "ollama" || m.providerName == "lmstudio" {
		costInfo = " (local)"
	} else if m.cost > 0 {
		costInfo = fmt.Sprintf(" ($%.4f)", m.cost)
	}

	var modeInfo string
	if m.session.Mode() == permission.ModeYOLO {
		if m.width < 60 {
			modeInfo = m.styles.ToolCardError.Bold(true).Render("[! YOLO] ")
		} else {
			modeInfo = m.styles.ToolCardError.Bold(true).Render("[! YOLO: AUTO-APPROVE ENABLED] ")
		}
	} else {
		modeInfo = fmt.Sprintf("[%s] ", m.session.Mode())
	}

	branchInfo := ""
	if m.branch != "" && m.width >= 80 {
		branchInfo = fmt.Sprintf("git:%s%s", m.branch, sep)
	}

	if m.isStreaming {
		status = fmt.Sprintf("%s%s%s%s %s", modeInfo, providerInfo, branchInfo, m.spinner.View(), m.activityLabel)
	} else if m.ctrlCPressed {
		status = m.styles.ErrorText.Render("Press Ctrl+C again to quit")
	} else {
		if m.width < 70 {
			status = m.styles.StatusBar.Render(fmt.Sprintf("%s%s%s%d/%d%s", modeInfo, m.modelName, sep, m.inputTokens, m.outputTokens, costInfo))
		} else {
			status = m.styles.StatusBar.Render(fmt.Sprintf("%s%s%s%s%stokens: %d in / %d out%s", modeInfo, providerInfo, branchInfo, m.activityLabel, sep, m.inputTokens, m.outputTokens, costInfo))
		}
	}

	if m.width > 0 && runewidth.StringWidth(StripANSI(status)) > m.width {
		status = truncateVisual(status, m.width)
	}

	if m.height < 20 {
		return fmt.Sprintf(
			"%s\n%s\n%s",
			m.viewport.View(),
			status,
			m.textarea.View(),
		)
	}

	toolToggle := "Ctrl+O: expand"
	if m.toolsExpanded {
		toolToggle = "Ctrl+O: collapse"
	}
	var help string
	if m.width < 60 {
		help = m.styles.HelpText.Render("Enter: send · Tab: mode · Esc: cancel · Ctrl+C: quit")
	} else {
		help = m.styles.HelpText.Render(fmt.Sprintf("Enter: send · Alt+Enter: newline · Tab: mode · Esc: cancel · %s · Ctrl+C: quit", toolToggle))
	}
	if isDumbTerminal() {
		help = strings.ReplaceAll(help, "·", "|")
	}
	if m.width > 0 && runewidth.StringWidth(StripANSI(help)) > m.width {
		help = truncateVisual(help, m.width)
	}

	return fmt.Sprintf(
		"%s\n%s\n%s\n%s",
		m.viewport.View(),
		status,
		m.textarea.View(),
		help,
	)
}

// ModelCatalogItem describes a model's basic properties for TUI display and validation.
type ModelCatalogItem struct {
	ID             string
	ContextWindow  int
	InputCostPerM  float64
	OutputCostPerM float64
}

func getModelCatalog(provider string) []ModelCatalogItem {
	switch strings.ToLower(provider) {
	case "anthropic":
		return []ModelCatalogItem{
			{ID: "claude-sonnet-5-5", ContextWindow: 1000000, InputCostPerM: 2.00, OutputCostPerM: 10.00},
			{ID: "claude-opus-5-5", ContextWindow: 1000000, InputCostPerM: 4.00, OutputCostPerM: 20.00},
			{ID: "claude-haiku-5-5", ContextWindow: 1000000, InputCostPerM: 0.10, OutputCostPerM: 0.50},
			{ID: "claude-fable-5-2", ContextWindow: 1000000, InputCostPerM: 8.00, OutputCostPerM: 40.00},
		}
	case "openai":
		return []ModelCatalogItem{
			{ID: "gpt-4o", ContextWindow: 128000, InputCostPerM: 2.50, OutputCostPerM: 10.00},
			{ID: "gpt-4o-mini", ContextWindow: 128000, InputCostPerM: 0.15, OutputCostPerM: 0.60},
			{ID: "o1", ContextWindow: 200000, InputCostPerM: 15.00, OutputCostPerM: 60.00},
			{ID: "o3-mini", ContextWindow: 200000, InputCostPerM: 1.10, OutputCostPerM: 4.40},
		}
	case "gemini":
		return []ModelCatalogItem{
			{ID: "gemini-2.5-pro", ContextWindow: 1048576, InputCostPerM: 1.25, OutputCostPerM: 10.00},
			{ID: "gemini-2.5-flash", ContextWindow: 1048576, InputCostPerM: 0.30, OutputCostPerM: 2.50},
		}
	case "ollama":
		return []ModelCatalogItem{
			{ID: "llama3.3", ContextWindow: 128000, InputCostPerM: 0, OutputCostPerM: 0},
			{ID: "qwen2.5-coder:7b", ContextWindow: 128000, InputCostPerM: 0, OutputCostPerM: 0},
			{ID: "deepseek-r1:7b", ContextWindow: 128000, InputCostPerM: 0, OutputCostPerM: 0},
		}
	case "lmstudio":
		return []ModelCatalogItem{
			{ID: "model", ContextWindow: 128000, InputCostPerM: 0, OutputCostPerM: 0},
			{ID: "qwen2.5-coder", ContextWindow: 128000, InputCostPerM: 0, OutputCostPerM: 0},
			{ID: "deepseek-coder", ContextWindow: 128000, InputCostPerM: 0, OutputCostPerM: 0},
		}
	default:
		return []ModelCatalogItem{
			{ID: config.DefaultModelForProvider(provider), ContextWindow: 128000, InputCostPerM: 0, OutputCostPerM: 0},
		}
	}
}
