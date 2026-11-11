package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ayushpatil0810/forge/internal/agent"
	"github.com/ayushpatil0810/forge/internal/permission"
)

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
	return styles.UserPrompt.Render("❯ " + u.text)
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

	pendingApproval  *agent.ApprovalRequestedPayload
	showApprovalDiff bool
	ctrlCPressed     bool
}

// New creates a new TUI model bound to a Session.
func New(s agent.Session, theme Theme) Model {
	ta := textarea.New()
	ta.Placeholder = "Ask Forge a question or describe a change... (Enter to send, Alt+Enter for newline)"
	ta.Focus()
	ta.Prompt = "❯ "
	ta.CharLimit = 16384
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false

	sp := spinner.New()
	sp.Spinner = spinner.Dot
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

func (m *Model) cyclePermissionMode() {
	curr := m.session.Mode()
	var next permission.Mode
	switch curr {
	case permission.ModeDefault: // "ask"
		next = permission.ModeAcceptEdits
	case permission.ModeAcceptEdits:
		next = permission.ModePlan
	case permission.ModePlan:
		next = permission.ModeYOLO
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
					targetMode := permission.Mode(parts[1])
					m.session.SetMode(targetMode)
				}
				m.items = append(m.items, userMessage{text: input})
				m.textarea.Reset()
				m.updateViewportContent()
				return m, nil
			}

			m.items = append(m.items, userMessage{text: input})
			m.textarea.Reset()
			m.isStreaming = true
			m.activityLabel = "Thinking..."
			m.currentResponse = ""
			m.updateViewportContent()

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
				if m.activityLabel == "Thinking..." || m.activityLabel == "Ready" {
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
					m.currentResponse = ""
				}
				tc := NewToolCard(ev.ToolStart.ID, ev.ToolStart.Name, ev.ToolStart.Input)
				m.toolCards[tc.ID] = tc
				m.items = append(m.items, toolMessage{card: tc})
				m.activityLabel = fmt.Sprintf("Running %s...", tc.Name)
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
				m.currentResponse = ""
			}
			m.updateViewportContent()

		case agent.EventError:
			m.isStreaming = false
			m.pendingApproval = nil
			m.textarea.Focus()
			m.activityLabel = "Error"
			errMsg := "unknown error"
			if ev.Err != nil {
				errMsg = ev.Err.Message
			}
			m.items = append(m.items, errorMessage{text: "Error: " + errMsg})
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

func (m *Model) updateViewportContent() {
	renderWidth := m.width
	if renderWidth <= 0 {
		renderWidth = 80
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
	if !m.ready {
		return "Initializing Forge TUI...\n"
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
	if m.cost > 0 {
		costInfo = fmt.Sprintf(" ($%.4f)", m.cost)
	}

	modeInfo := fmt.Sprintf("[%s] ", m.session.Mode())

	if m.isStreaming {
		status = fmt.Sprintf("%s%s%s %s", modeInfo, providerInfo, m.spinner.View(), m.activityLabel)
	} else if m.ctrlCPressed {
		status = m.styles.ErrorText.Render("Press Ctrl+C again to quit")
	} else {
		status = m.styles.StatusBar.Render(fmt.Sprintf("%s%s%s · tokens: %d in / %d out%s", modeInfo, providerInfo, m.activityLabel, m.inputTokens, m.outputTokens, costInfo))
	}

	toolToggle := "Ctrl+O: expand tools"
	if m.toolsExpanded {
		toolToggle = "Ctrl+O: collapse tools"
	}
	help := m.styles.HelpText.Render(fmt.Sprintf("Enter: send · Alt+Enter: newline · Tab: mode · Esc: cancel · %s · Ctrl+C: quit", toolToggle))

	return fmt.Sprintf(
		"%s\n%s\n%s\n%s",
		m.viewport.View(),
		status,
		m.textarea.View(),
		help,
	)
}
