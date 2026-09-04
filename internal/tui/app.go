// Package tui is repohop's Bubble Tea program: a stack of screens sharing a
// header, a footer key bar and one help overlay.
package tui

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rustbohr/repohop/internal/config"
	"github.com/rustbohr/repohop/internal/logging"
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

// keyCapturer is implemented by screens that are taking text input. While one
// says it is capturing, the app forwards every key except ctrl+c straight to
// it, so a field can contain a "?" and esc can mean "leave this field" rather
// than "leave this screen".
type keyCapturer interface {
	capturesKeys() bool
}

// capturing reports whether the top screen is currently taking raw keys.
func (a *App) capturing() bool {
	capturer, ok := a.top().(keyCapturer)
	return ok && capturer.capturesKeys()
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
	crash *crash
}

// crash is a panic repohop caught and turned into a message. A bug in one
// screen should cost the user their place, not their session.
type crash struct {
	when  time.Time
	doing string
	value any
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
	// projectSavedMsg reports that a project was written to the config file.
	projectSavedMsg struct{ previous, name string }
	// projectDeletedMsg reports that a project was removed from the config.
	projectDeletedMsg struct{ name string }
)

func push(s screen) tea.Cmd  { return func() tea.Msg { return pushMsg{s} } }
func pop() tea.Msg           { return popMsg{} }
func flash(s string) tea.Cmd { return func() tea.Msg { return flashMsg(s) } }

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
	if err != nil {
		// A panic that escapes a command goroutine never reaches the app's own
		// recovery, so record what little is known about it here.
		logging.Log().Error("running the interface", err)
	}
	return err
}

func (a *App) Init() tea.Cmd { return a.top().Init() }

func (a *App) top() screen { return a.stack[len(a.stack)-1] }

func (a *App) Update(msg tea.Msg) (next tea.Model, cmd tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			a.recordCrash("handling "+msgName(msg), r)
			next, cmd = a, nil
		}
	}()

	// While a crash is on screen, the next key dismisses it.
	if a.crash != nil {
		if key, ok := msg.(tea.KeyMsg); ok {
			if key.String() == "ctrl+c" {
				return a, tea.Quit
			}
			a.crash = nil
			return a, nil
		}
	}

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

	case projectSavedMsg:
		if err := a.sh.cfg.Reload(); err != nil {
			a.err = err
			return a, nil
		}
		if msg.previous != msg.name {
			if state, err := config.LoadState(); err == nil && state.ActiveProject == msg.previous {
				_ = config.SaveState(config.State{ActiveProject: msg.name})
			}
		}
		if saved, ok := a.sh.cfg.Project(msg.name); ok && a.sh.project.Name == msg.previous {
			a.sh.project = saved
		}
		a.stack = a.stack[:len(a.stack)-1]
		return a, tea.Batch(func() tea.Msg { return refreshMsg{} }, flash("saved "+msg.name))

	case projectDeletedMsg:
		if err := a.sh.cfg.Reload(); err != nil {
			a.err = err
			return a, nil
		}
		if state, err := config.LoadState(); err == nil && state.ActiveProject == msg.name {
			_ = config.SaveState(config.State{})
		}
		if a.sh.project.Name == msg.name {
			a.sh.project = model.Project{}
		}
		return a, flash("deleted " + msg.name)

	case flashMsg:
		a.flash = string(msg)
		return a, nil

	case errMsg:
		a.err = msg.err
		logging.Log().Error("on the "+a.top().Title()+" screen", msg.err)
		return a, nil
	}

	updated, cmd := a.top().Update(msg)
	a.stack[len(a.stack)-1] = updated
	return a, cmd
}

// globalKey handles the keys that mean the same thing on every screen.
func (a *App) globalKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.String() == "ctrl+c" {
		return tea.Quit, true
	}
	if a.capturing() && !a.help {
		return nil, false
	}

	switch msg.String() {
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

func (a *App) View() (out string) {
	defer func() {
		if r := recover(); r != nil {
			a.recordCrash("drawing the "+a.top().Title()+" screen", r)
			out = strings.Join([]string{a.header(), a.crashView(), a.footer()}, "\n")
		}
	}()

	var body string
	switch {
	case a.crash != nil:
		body = a.crashView()
	case a.help:
		body = a.helpOverlay()
	default:
		body = a.top().View()
	}
	return strings.Join([]string{a.header(), body, a.footer()}, "\n")
}

// recordCrash logs a recovered panic and leaves the app on a screen the user
// can carry on from.
func (a *App) recordCrash(doing string, recovered any) {
	logging.Log().Panic(doing, recovered, debug.Stack())
	a.crash = &crash{when: time.Now(), doing: doing, value: recovered}
	a.help = false

	// The screen that failed is the one to leave, if there is somewhere to go.
	if len(a.stack) > 1 {
		a.stack = a.stack[:len(a.stack)-1]
	}
}

// crashView explains what happened in the terms the user needs: what broke,
// that it is not their fault, where the details are, and how to carry on.
func (a *App) crashView() string {
	t := a.sh.theme
	lines := []string{
		"",
		"  " + t.Failure.Render("Something went wrong "+a.crash.doing) + ".",
		"",
		"  " + fmt.Sprint(a.crash.value),
		"",
		"  " + t.Muted.Render("This is a bug in repohop, not something you did."),
	}
	if path := logging.Log().Path(); path != "" {
		lines = append(lines,
			"  "+t.Muted.Render("The details are in ")+shortenHome(path))
	}
	lines = append(lines,
		"",
		"  "+t.Key.Render("any key")+" "+t.Footer.Render("carry on")+
			t.Footer.Render(" · ")+t.Key.Render("ctrl+c")+" "+t.Footer.Render("quit"))
	return strings.Join(lines, "\n")
}

// msgName describes a message for the log without dragging in its contents.
func msgName(msg tea.Msg) string {
	if key, ok := msg.(tea.KeyMsg); ok {
		return "key " + key.String()
	}
	return fmt.Sprintf("%T", msg)
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
	if a.crash != nil {
		return t.Failure.Render("recovered from an error")
	}
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
		fmt.Fprintf(&b, "  %s  %s\n", t.Key.Render(cell(hint.Key, 8)), hint.Desc)
	}
	b.WriteString("\n" + t.Title.Render("everywhere") + "\n\n")
	for _, hint := range globalHints() {
		fmt.Fprintf(&b, "  %s  %s\n", t.Key.Render(cell(hint.Key, 8)), hint.Desc)
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
