// Package tui is repohop's Bubble Tea program: a stack of screens sharing a
// header, a footer key bar and one help overlay.
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rustbohr/repohop/internal/config"
	"github.com/rustbohr/repohop/internal/model"
	"github.com/rustbohr/repohop/internal/ops"
)

// screen is one page of the UI. Screens own their own layout inside the body
// area the app gives them.
type screen interface {
	Init() tea.Cmd
	Update(tea.Msg) (screen, tea.Cmd)
	View() string
	Title() string
	// Hints are the footer key reminders, in display order.
	Hints() []Hint
}

// Hint is one "key — what it does" pair in the footer and the help overlay.
type Hint struct {
	Key  string
	Desc string
}

// shared is the context every screen needs. Screens hold a pointer, so a
// project chosen on one screen is visible to the next.
type shared struct {
	cfg     *config.Config
	runner  *ops.Runner
	theme   Theme
	project model.Project
	// ctx is cancelled when the program quits, so in-flight git commands stop.
	ctx context.Context
	// width and height are the body area, updated on every resize.
	width, height int
}

// App is the root model.
type App struct {
	sh    *shared
	stack []screen
	help  bool
	flash string
	err   error
}

// Messages the screens send back to the app.
type (
	// pushMsg puts a new screen on top of the stack.
	pushMsg struct{ screen screen }
	// popMsg returns to the previous screen.
	popMsg struct{}
	// flashMsg shows a transient note in the footer.
	flashMsg string
	// errMsg reports an error that is not tied to a single repository.
	errMsg struct{ err error }
	// projectChosenMsg switches the active project.
	projectChosenMsg struct{ project model.Project }
	// refreshMsg asks the screen underneath to reload after an operation.
	refreshMsg struct{}
)

func push(s screen) tea.Cmd  { return func() tea.Msg { return pushMsg{s} } }
func pop() tea.Msg           { return popMsg{} }
func flash(s string) tea.Cmd { return func() tea.Msg { return flashMsg(s) } }
func reportErr(err error) tea.Cmd {
	return func() tea.Msg { return errMsg{err} }
}

// New builds the program's root model. When the configuration holds exactly
// one project the project list is skipped entirely.
func New(ctx context.Context, cfg *config.Config, project model.Project) *App {
	sh := &shared{
		cfg:     cfg,
		runner:  ops.New(cfg.Settings.Concurrency),
		theme:   NewTheme(),
		project: project,
		ctx:     ctx,
		width:   80,
		height:  20,
	}

	app := &App{sh: sh}
	if project.Name == "" {
		app.stack = []screen{newProjectList(sh)}
	} else {
		app.stack = []screen{newDashboard(sh)}
	}
	return app
}

// Run starts the Bubble Tea program.
func Run(ctx context.Context, cfg *config.Config, project model.Project) error {
	program := tea.NewProgram(New(ctx, cfg, project), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := program.Run()
	return err
}

func (a *App) Init() tea.Cmd { return a.top().Init() }

func (a *App) top() screen { return a.stack[len(a.stack)-1] }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.sh.width = msg.Width
		a.sh.height = max(msg.Height-chromeHeight, 1)
		// Screens lay out from shared, but still want the event.

	case tea.KeyMsg:
		if cmd, handled := a.globalKey(msg); handled {
			return a, cmd
		}

	case pushMsg:
		a.flash = ""
		a.stack = append(a.stack, msg.screen)
		return a, msg.screen.Init()

	case popMsg:
		if len(a.stack) == 1 {
			return a, tea.Quit
		}
		a.flash = ""
		a.stack = a.stack[:len(a.stack)-1]
		return a, func() tea.Msg { return refreshMsg{} }

	case projectChosenMsg:
		a.sh.project = msg.project
		_ = config.SaveState(config.State{ActiveProject: msg.project.Name})
		a.flash = ""
		a.stack = append(a.stack, newDashboard(a.sh))
		return a, a.top().Init()

	case flashMsg:
		a.flash = string(msg)
		return a, nil

	case errMsg:
		a.err = msg.err
		return a, nil
	}

	updated, cmd := a.top().Update(msg)
	a.stack[len(a.stack)-1] = updated
	return a, cmd
}

// globalKey handles the keys that mean the same thing on every screen.
func (a *App) globalKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit, true
	case "?":
		a.help = !a.help
		return nil, true
	case "esc":
		if a.help {
			a.help = false
			return nil, true
		}
		if len(a.stack) == 1 {
			return nil, false // the root screen decides what esc means
		}
		return pop, true
	}
	if a.help {
		// While the overlay is up, any other key dismisses it.
		a.help = false
		return nil, true
	}
	return nil, false
}

// chromeHeight is the header plus footer the app draws around every screen.
const chromeHeight = 4

func (a *App) View() string {
	body := a.top().View()
	if a.help {
		body = a.helpOverlay()
	}
	return strings.Join([]string{a.header(), body, a.footer()}, "\n")
}

func (a *App) header() string {
	t := a.sh.theme
	left := t.Header.Render("repohop")
	var parts []string
	if a.sh.project.Name != "" {
		parts = append(parts, a.sh.project.Name, plural(len(a.sh.project.Repos), "repo"))
	}
	parts = append(parts, a.top().Title())

	line := left + t.HeaderDim.Render(" · "+strings.Join(parts, " · "))
	return line + "\n" + t.HeaderDim.Render(strings.Repeat("─", max(a.sh.width, 1)))
}

func (a *App) footer() string {
	t := a.sh.theme
	if a.err != nil {
		return t.Failure.Render("error: " + a.err.Error())
	}
	if a.flash != "" {
		return t.Warning.Render(a.flash)
	}

	hints := append(a.top().Hints(), Hint{"?", "help"})
	parts := make([]string, 0, len(hints))
	for _, hint := range hints {
		parts = append(parts, t.Key.Render(hint.Key)+" "+t.Footer.Render(hint.Desc))
	}
	return truncate(strings.Join(parts, t.Footer.Render(" · ")), a.sh.width)
}

func (a *App) helpOverlay() string {
	t := a.sh.theme
	var b strings.Builder
	b.WriteString(t.Title.Render(a.top().Title()+" keys") + "\n\n")
	for _, hint := range a.top().Hints() {
		fmt.Fprintf(&b, "  %s  %s\n", t.Key.Render(pad(hint.Key, 8)), hint.Desc)
	}
	b.WriteString("\n" + t.Title.Render("everywhere") + "\n\n")
	for _, hint := range globalHints() {
		fmt.Fprintf(&b, "  %s  %s\n", t.Key.Render(pad(hint.Key, 8)), hint.Desc)
	}
	b.WriteString("\n" + t.Muted.Render("any key closes this overlay"))
	return lipgloss.NewStyle().MaxHeight(a.sh.height).Render(t.Overlay.Render(b.String()))
}

func globalHints() []Hint {
	return []Hint{
		{"?", "toggle this help"},
		{"esc", "back one screen"},
		{"q", "quit from the first screen"},
		{"ctrl+c", "abort immediately"},
	}
}
