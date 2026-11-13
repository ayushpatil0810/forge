package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ayushpatil0810/forge/themes"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

type Theme struct {
	Name         string         `json:"name"`
	IsDark       bool           `json:"is_dark"`
	Background   lipgloss.Color `json:"background"`
	Foreground   lipgloss.Color `json:"foreground"`
	Primary      lipgloss.Color `json:"primary"`
	Secondary    lipgloss.Color `json:"secondary"`
	Accent       lipgloss.Color `json:"accent"`
	Muted        lipgloss.Color `json:"muted"`
	Error        lipgloss.Color `json:"error"`
	Success      lipgloss.Color `json:"success"`
	Warning      lipgloss.Color `json:"warning"`
	Border       lipgloss.Color `json:"border"`
	GlamourStyle string         `json:"glamour_style"`
}

type themeRaw struct {
	Name         string `json:"name"`
	IsDark       bool   `json:"is_dark"`
	Background   string `json:"background"`
	Foreground   string `json:"foreground"`
	Primary      string `json:"primary"`
	Secondary    string `json:"secondary"`
	Accent       string `json:"accent"`
	Muted        string `json:"muted"`
	Error        string `json:"error"`
	Success      string `json:"success"`
	Warning      string `json:"warning"`
	Border       string `json:"border"`
	GlamourStyle string `json:"glamour_style"`
}

func loadFromFS(filename string) (Theme, error) {
	data, err := themes.FS.ReadFile(filename)
	if err != nil {
		return Theme{}, err
	}
	var raw themeRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return Theme{}, fmt.Errorf("parsing theme %q: %w", filename, err)
	}
	return Theme{
		Name:         raw.Name,
		IsDark:       raw.IsDark,
		Background:   lipgloss.Color(raw.Background),
		Foreground:   lipgloss.Color(raw.Foreground),
		Primary:      lipgloss.Color(raw.Primary),
		Secondary:    lipgloss.Color(raw.Secondary),
		Accent:       lipgloss.Color(raw.Accent),
		Muted:        lipgloss.Color(raw.Muted),
		Error:        lipgloss.Color(raw.Error),
		Success:      lipgloss.Color(raw.Success),
		Warning:      lipgloss.Color(raw.Warning),
		Border:       lipgloss.Color(raw.Border),
		GlamourStyle: raw.GlamourStyle,
	}, nil
}

// LoadTheme loads a theme by name with automatic environment and fallback handling.
func LoadTheme(name string) (Theme, error) {
	// Respect NO_COLOR standard (https://no-color.org/) and TERM=dumb
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		lipgloss.SetColorProfile(termenv.Ascii)
		return MonochromeTheme(), nil
	}

	norm := strings.ToLower(strings.TrimSpace(name))
	switch norm {
	case "", "auto":
		return AdaptiveTheme(), nil
	case "monochrome":
		lipgloss.SetColorProfile(termenv.Ascii)
		return MonochromeTheme(), nil
	case "dark":
		return DarkTheme(), nil
	case "light":
		return LightTheme(), nil
	case "catppuccin-mocha", "mocha":
		return loadFromFS("catppuccin-mocha.json")
	case "tokyo-night", "tokyonight":
		return loadFromFS("tokyo-night.json")
	default:
		t, err := loadFromFS(norm + ".json")
		if err == nil {
			return t, nil
		}
		return DarkTheme(), fmt.Errorf("unknown theme %q (available: dark, light, catppuccin-mocha, tokyo-night, auto, monochrome)", name)
	}
}

func DarkTheme() Theme {
	t, err := loadFromFS("dark.json")
	if err == nil {
		return t
	}
	return Theme{
		Name:         "dark",
		IsDark:       true,
		Background:   lipgloss.Color("#1e1e2e"),
		Foreground:   lipgloss.Color("#cdd6f4"),
		Primary:      lipgloss.Color("#89b4fa"),
		Secondary:    lipgloss.Color("#cba6f7"),
		Accent:       lipgloss.Color("#f5c2e7"),
		Muted:        lipgloss.Color("#a6adc8"),
		Error:        lipgloss.Color("#f38ba8"),
		Success:      lipgloss.Color("#a6e3a1"),
		Warning:      lipgloss.Color("#f9e2af"),
		Border:       lipgloss.Color("#45475a"),
		GlamourStyle: "dark",
	}
}

func LightTheme() Theme {
	t, err := loadFromFS("light.json")
	if err == nil {
		return t
	}
	return Theme{
		Name:         "light",
		IsDark:       false,
		Background:   lipgloss.Color("#ffffff"),
		Foreground:   lipgloss.Color("#1a1a2e"),
		Primary:      lipgloss.Color("#0052cc"),
		Secondary:    lipgloss.Color("#5e35b1"),
		Accent:       lipgloss.Color("#d81b60"),
		Muted:        lipgloss.Color("#495057"),
		Error:        lipgloss.Color("#c92a2a"),
		Success:      lipgloss.Color("#247434"),
		Warning:      lipgloss.Color("#b03800"),
		Border:       lipgloss.Color("#ced4da"),
		GlamourStyle: "light",
	}
}

func MonochromeTheme() Theme {
	return Theme{
		Name:         "monochrome",
		IsDark:       true,
		Background:   lipgloss.Color(""),
		Foreground:   lipgloss.Color(""),
		Primary:      lipgloss.Color(""),
		Secondary:    lipgloss.Color(""),
		Accent:       lipgloss.Color(""),
		Muted:        lipgloss.Color(""),
		Error:        lipgloss.Color(""),
		Success:      lipgloss.Color(""),
		Warning:      lipgloss.Color(""),
		Border:       lipgloss.Color(""),
		GlamourStyle: "ascii",
	}
}

func AdaptiveTheme() Theme {
	if termenv.HasDarkBackground() {
		return DarkTheme()
	}
	return LightTheme()
}

type Styles struct {
	UserPrompt      lipgloss.Style
	AssistantText   lipgloss.Style
	SystemInfo      lipgloss.Style
	ErrorText       lipgloss.Style
	StatusBar       lipgloss.Style
	HelpText        lipgloss.Style
	BorderBox       lipgloss.Style
	ActivityLabel   lipgloss.Style
	ToolCardSuccess lipgloss.Style
	ToolCardError   lipgloss.Style
	ToolCardRunning lipgloss.Style
	ToolCardOutput  lipgloss.Style
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
		ToolCardSuccess: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Success),
		ToolCardError: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Error),
		ToolCardRunning: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Accent),
		ToolCardOutput: lipgloss.NewStyle().
			Foreground(t.Muted),
	}
}
