package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

type Theme struct {
	IsDark       bool
	Background   lipgloss.Color
	Foreground   lipgloss.Color
	Primary      lipgloss.Color
	Secondary    lipgloss.Color
	Accent       lipgloss.Color
	Muted        lipgloss.Color
	Error        lipgloss.Color
	Success      lipgloss.Color
	Warning      lipgloss.Color
	Border       lipgloss.Color
	GlamourStyle string
}

func DarkTheme() Theme {
	return Theme{
		IsDark:       true,
		Background:   lipgloss.Color("#1e1e2e"),
		Foreground:   lipgloss.Color("#cdd6f4"),
		Primary:      lipgloss.Color("#89b4fa"),
		Secondary:    lipgloss.Color("#cba6f7"),
		Accent:       lipgloss.Color("#f5c2e7"),
		Muted:        lipgloss.Color("#6c7086"),
		Error:        lipgloss.Color("#f38ba8"),
		Success:      lipgloss.Color("#a6e3a1"),
		Warning:      lipgloss.Color("#f9e2af"),
		Border:       lipgloss.Color("#45475a"),
		GlamourStyle: "dark",
	}
}

func LightTheme() Theme {
	return Theme{
		IsDark:       false,
		Background:   lipgloss.Color("#eff1f5"),
		Foreground:   lipgloss.Color("#4c4f69"),
		Primary:      lipgloss.Color("#1e66f5"),
		Secondary:    lipgloss.Color("#8839ef"),
		Accent:       lipgloss.Color("#ea76cb"),
		Muted:        lipgloss.Color("#9ca0b0"),
		Error:        lipgloss.Color("#d20f39"),
		Success:      lipgloss.Color("#40a02b"),
		Warning:      lipgloss.Color("#df8e1d"),
		Border:       lipgloss.Color("#bcc0cc"),
		GlamourStyle: "light",
	}
}

func AdaptiveTheme() Theme {
	if termenv.HasDarkBackground() {
		return DarkTheme()
	}
	return LightTheme()
}

type Styles struct {
	UserPrompt    lipgloss.Style
	AssistantText lipgloss.Style
	SystemInfo    lipgloss.Style
	ErrorText     lipgloss.Style
	StatusBar     lipgloss.Style
	HelpText      lipgloss.Style
	BorderBox     lipgloss.Style
	ActivityLabel lipgloss.Style
}

func NewStyles(t Theme) Styles {
	return Styles{
		UserPrompt: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Primary),
		AssistantText: lipgloss.NewStyle().
			Foreground(t.Foreground),
		SystemInfo: lipgloss.NewStyle().
			Italic(true).
			Foreground(t.Muted),
		ErrorText: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Error),
		StatusBar: lipgloss.NewStyle().
			Foreground(t.Muted),
		HelpText: lipgloss.NewStyle().
			Foreground(t.Muted),
		BorderBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Border),
		ActivityLabel: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Secondary),
	}
}
