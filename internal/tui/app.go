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
)

type sessionClosedMsg struct{}
type renderTickMsg struct{}
type resetCtrlCMsg struct{}

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

	messages        []string
	currentResponse string
	activityLabel   string
	isStreaming     bool
	renderPending   bool
	inputTokens     int
	outputTokens    int

	ctrlCPressed bool
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
	}
	return m
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
			m.viewport.SetContent(strings.Join(m.messages, "\n\n"))
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = viewportHeight
		}

		m.textarea.SetWidth(msg.Width - 2)
		m.updateViewportContent()

	case tea.KeyMsg:
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

		case tea.KeyEsc:
			if m.isStreaming {
				m.session.Cancel()
				m.isStreaming = false
				m.activityLabel = "Interrupted"
				if m.currentResponse != "" {
					rendered, _ := m.renderer.Render(m.currentResponse + "\n\n*[Interrupted]*")
					m.messages = append(m.messages, rendered)
					m.currentResponse = ""
					m.updateViewportContent()
				}
				return m, nil
			}

		case tea.KeyEnter:
			// Alt+Enter or Shift+Enter inserts newline; Enter sends
			if msg.Alt {
				m.textarea.InsertString("\n")
				return m, nil
			}

			input := strings.TrimSpace(m.textarea.Value())
			if input == "" || m.isStreaming {
				return m, nil
			}

			userBlock := m.styles.UserPrompt.Render("❯ " + input)
			m.messages = append(m.messages, userBlock)
			m.textarea.Reset()
			m.isStreaming = true
			m.activityLabel = "Thinking..."
			m.currentResponse = ""
			m.updateViewportContent()

			if err := m.session.Send(context.Background(), input); err != nil {
				m.isStreaming = false
				m.messages = append(m.messages, m.styles.ErrorText.Render("Send error: "+err.Error()))
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

		case agent.EventUsageUpdated:
			if ev.Usage != nil {
				m.inputTokens = ev.Usage.InputTokens
				m.outputTokens = ev.Usage.OutputTokens
			}

		case agent.EventDone:
			m.isStreaming = false
			m.activityLabel = "Ready"
			if m.currentResponse != "" {
				if ev.Done != nil && ev.Done.Reason == "cancelled" {
					m.currentResponse += "\n\n*[Interrupted]*"
				}
				rendered, _ := m.renderer.Render(m.currentResponse)
				m.messages = append(m.messages, rendered)
				m.currentResponse = ""
			}
			m.updateViewportContent()

		case agent.EventError:
			m.isStreaming = false
			m.activityLabel = "Error"
			errMsg := "unknown error"
			if ev.Err != nil {
				errMsg = ev.Err.Message
			}
			m.messages = append(m.messages, m.styles.ErrorText.Render("Error: "+errMsg))
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
	var fullText strings.Builder
	for i, msg := range m.messages {
		if i > 0 {
			fullText.WriteString("\n\n")
		}
		fullText.WriteString(msg)
	}

	if m.currentResponse != "" {
		if fullText.Len() > 0 {
			fullText.WriteString("\n\n")
		}
		rendered, _ := m.renderer.Render(m.currentResponse)
		fullText.WriteString(rendered)
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
	if m.isStreaming {
		status = fmt.Sprintf("%s %s", m.spinner.View(), m.activityLabel)
	} else if m.ctrlCPressed {
		status = m.styles.ErrorText.Render("Press Ctrl+C again to quit")
	} else {
		status = m.styles.StatusBar.Render(fmt.Sprintf("%s · tokens: %d in / %d out", m.activityLabel, m.inputTokens, m.outputTokens))
	}

	help := m.styles.HelpText.Render("Enter: send · Alt+Enter: newline · Esc: cancel · Ctrl+C: quit")

	return fmt.Sprintf(
		"%s\n%s\n%s\n%s",
		m.viewport.View(),
		status,
		m.textarea.View(),
		help,
	)
}
