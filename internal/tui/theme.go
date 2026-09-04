package tui

import "github.com/charmbracelet/lipgloss"

// Theme holds every style the screens use. Colours are adaptive so the UI
// stays legible on light and dark terminals, and lipgloss degrades them on
// terminals that cannot render them.
type Theme struct {
	Header    lipgloss.Style
	HeaderDim lipgloss.Style
	Footer    lipgloss.Style
	Key       lipgloss.Style
	Title     lipgloss.Style

	Row         lipgloss.Style
	SelectedRow lipgloss.Style
	Cursor      lipgloss.Style
	ColumnHead  lipgloss.Style

	Clean    lipgloss.Style
	Dirty    lipgloss.Style
	Failure  lipgloss.Style
	Success  lipgloss.Style
	Warning  lipgloss.Style
	Muted    lipgloss.Style
	Match    lipgloss.Style
	Overlay  lipgloss.Style
	Preview  lipgloss.Style
	Spinner  lipgloss.Style
	Selected lipgloss.Style
}

// Colours, kept in one place. Adaptive pairs are (light terminal, dark
// terminal).
var (
	colAccent  = lipgloss.AdaptiveColor{Light: "#005f87", Dark: "#7dcfff"}
	colMuted   = lipgloss.AdaptiveColor{Light: "#6c6c6c", Dark: "#8a8a8a"}
	colGood    = lipgloss.AdaptiveColor{Light: "#187d18", Dark: "#9ece6a"}
	colWarn    = lipgloss.AdaptiveColor{Light: "#8f5c00", Dark: "#e0af68"}
	colBad     = lipgloss.AdaptiveColor{Light: "#a00000", Dark: "#f7768e"}
	colBorder  = lipgloss.AdaptiveColor{Light: "#bcbcbc", Dark: "#3b4261"}
	colInverse = lipgloss.AdaptiveColor{Light: "#eeeeee", Dark: "#1a1b26"}
)

// NewTheme builds the default theme.
func NewTheme() Theme {
	return Theme{
		Header:      lipgloss.NewStyle().Bold(true).Foreground(colAccent),
		HeaderDim:   lipgloss.NewStyle().Foreground(colMuted),
		Footer:      lipgloss.NewStyle().Foreground(colMuted),
		Key:         lipgloss.NewStyle().Bold(true).Foreground(colAccent),
		Title:       lipgloss.NewStyle().Bold(true),
		Row:         lipgloss.NewStyle(),
		SelectedRow: lipgloss.NewStyle().Bold(true),
		Cursor:      lipgloss.NewStyle().Bold(true).Foreground(colAccent),
		ColumnHead:  lipgloss.NewStyle().Bold(true).Foreground(colMuted),
		Clean:       lipgloss.NewStyle().Foreground(colGood),
		Dirty:       lipgloss.NewStyle().Foreground(colWarn),
		Failure:     lipgloss.NewStyle().Foreground(colBad),
		Success:     lipgloss.NewStyle().Foreground(colGood),
		Warning:     lipgloss.NewStyle().Foreground(colWarn),
		Muted:       lipgloss.NewStyle().Foreground(colMuted),
		Match:       lipgloss.NewStyle().Bold(true).Foreground(colAccent),
		Overlay:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colBorder).Padding(0, 1),
		Preview:     lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(colBorder).PaddingLeft(1),
		Spinner:     lipgloss.NewStyle().Foreground(colAccent),
		Selected:    lipgloss.NewStyle().Foreground(colAccent).Background(colInverse),
	}
}
