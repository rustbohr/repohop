package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rustbohr/repohop/internal/config"
	"github.com/rustbohr/repohop/internal/model"
	"github.com/rustbohr/repohop/internal/scan"
)

// setupStep is where the setup flow has got to.
type setupStep int

const (
	stepRoot setupStep = iota
	stepScanning
	stepChoose
	stepName
)

// setup builds a project from repositories found on disk, so a new user is
// productive without hand-writing config.
type setup struct {
	sh   *shared
	step setupStep
	path *pathInput
	name textinput.Model
	spin spinner.Model

	root     string
	found    []scan.Repo
	selected map[int]bool
	cursor   int
	offset   int
	err      error
}

type scanDoneMsg struct {
	root  string
	found []scan.Repo
	err   error
}

func newSetup(sh *shared) *setup {
	name := textinput.New()
	name.Prompt = "  "
	name.Placeholder = "project name"

	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	spin.Style = sh.theme.Spinner

	return &setup{
		sh:       sh,
		path:     newPathInput(sh.theme, defaultScanRoot()),
		name:     name,
		spin:     spin,
		selected: map[int]bool{},
	}
}

// defaultScanRoot is the directory the path field starts on: the first of the
// usual homes for checkouts that actually exists, else the home directory.
func defaultScanRoot() string {
	for _, candidate := range []string{"~/src", "~/dev", "~/code", "~/projects"} {
		if info, err := os.Stat(config.ExpandPath(candidate)); err == nil && info.IsDir() {
			return candidate
		}
	}
	return "~"
}

// capturesKeys keeps the app's global keys out of the text fields.
func (s *setup) capturesKeys() bool {
	return s.step == stepRoot || s.step == stepName
}

func (s *setup) Init() tea.Cmd { return textinput.Blink }

func (s *setup) Title() string { return "set up a project" }

func (s *setup) Hints() []Hint {
	switch s.step {
	case stepRoot:
		return append(s.path.Hints(), Hint{"enter", "scan"}, Hint{"esc", "cancel"})
	case stepScanning:
		return []Hint{{"esc", "cancel"}}
	case stepChoose:
		return []Hint{{"space", "select"}, {"a", "all"}, {"enter", "continue"}, {"esc", "back"}}
	default:
		return []Hint{{"enter", "save"}, {"esc", "back"}}
	}
}

func (s *setup) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case scanDoneMsg:
		s.err = msg.err
		s.root = msg.root
		s.found = msg.found
		s.step = stepChoose
		for i := range s.found {
			s.selected[i] = true
		}
		return s, nil

	case spinner.TickMsg:
		if s.step != stepScanning {
			return s, nil
		}
		var cmd tea.Cmd
		s.spin, cmd = s.spin.Update(msg)
		return s, cmd

	case tea.KeyMsg:
		return s.key(msg)
	}
	return s, nil
}

func (s *setup) key(msg tea.KeyMsg) (screen, tea.Cmd) {
	switch s.step {
	case stepRoot:
		if cmd, consumed := s.path.Update(msg); consumed {
			return s, cmd
		}
		switch msg.String() {
		case "enter":
			s.step = stepScanning
			return s, tea.Batch(s.spin.Tick, s.scan(s.path.Path()))
		case "esc":
			return s, pop
		}
		return s, nil

	case stepChoose:
		switch msg.String() {
		case "up", "k":
			s.cursor = max(s.cursor-1, 0)
		case "down", "j":
			s.cursor = min(s.cursor+1, max(len(s.found)-1, 0))
		case " ":
			s.selected[s.cursor] = !s.selected[s.cursor]
			s.cursor = min(s.cursor+1, max(len(s.found)-1, 0))
		case "a":
			s.toggleAll()
		case "enter":
			if s.count() == 0 {
				return s, flash("select at least one repository")
			}
			s.step = stepName
			if s.name.Value() == "" {
				s.name.SetValue(filepath.Base(s.root))
			}
			s.name.Focus()
			s.name.CursorEnd()
			return s, textinput.Blink
		}
		return s, nil

	case stepName:
		switch msg.String() {
		case "enter":
			return s, s.save()
		case "esc":
			s.step = stepChoose
			return s, nil
		}
		var cmd tea.Cmd
		s.name, cmd = s.name.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s *setup) scan(root string) tea.Cmd {
	return func() tea.Msg {
		found, err := scan.Find(s.sh.ctx, scan.Options{Root: root})
		return scanDoneMsg{root: root, found: found, err: err}
	}
}

func (s *setup) toggleAll() {
	all := s.count() == len(s.found)
	for i := range s.found {
		s.selected[i] = !all
	}
}

func (s *setup) count() int {
	n := 0
	for i := range s.found {
		if s.selected[i] {
			n++
		}
	}
	return n
}

// save writes the project into the user's config file and opens it.
func (s *setup) save() tea.Cmd {
	name := strings.TrimSpace(s.name.Value())
	if name == "" {
		return flash("the project needs a name")
	}

	spec := config.ProjectSpec{Name: name, Base: s.root}
	project := model.Project{Name: name, Base: s.root, Source: s.sh.cfg.UserPath}
	for i, repo := range s.found {
		if !s.selected[i] {
			continue
		}
		spec.Repos = append(spec.Repos, config.RepoSpec{Path: repo.Rel})
		project.Repos = append(project.Repos, model.Repo{Name: repo.Name, Path: repo.Path})
	}

	return func() tea.Msg {
		if err := config.AddProject(s.sh.cfg.UserPath, spec); err != nil {
			return errMsg{err}
		}
		// Reload so the project list reflects what is now on disk.
		if err := s.sh.cfg.Reload(); err == nil {
			if saved, ok := s.sh.cfg.Project(name); ok {
				project = saved
			}
		}
		return projectChosenMsg{project: project}
	}
}

func (s *setup) View() string {
	t := s.sh.theme
	switch s.step {
	case stepRoot:
		s.path.SetHeight(max(s.sh.height-5, 3))
		if s.path.browsing() {
			return "\n" + s.path.View()
		}
		return strings.Join([]string{
			"",
			"  Which directory holds your repositories?",
			"  " + t.Muted.Render("repohop looks up to "+itoa(scan.DefaultDepth)+" levels below it."),
			"",
			s.path.View(),
		}, "\n")

	case stepScanning:
		return "\n  " + s.spin.View() + " " + t.Muted.Render("scanning "+s.root)

	case stepChoose:
		return s.viewChoose()

	default:
		return strings.Join([]string{
			"",
			"  Name this project.",
			"  " + t.Muted.Render(plural(s.count(), "repository")+" under "+s.root),
			"",
			s.name.View(),
			"",
			"  " + t.Muted.Render("saved to "+s.sh.cfg.UserPath),
		}, "\n")
	}
}

func (s *setup) viewChoose() string {
	t := s.sh.theme
	if s.err != nil {
		return "\n  " + t.Failure.Render(s.err.Error())
	}
	if len(s.found) == 0 {
		return "\n  " + t.Muted.Render("no git repositories under "+s.root) + "\n\n  " +
			t.Footer.Render("press esc and try another directory")
	}

	var b strings.Builder
	b.WriteString("\n  " + t.Muted.Render(plural(len(s.found), "repository")+" under "+s.root) + "\n\n")

	visible := max(s.sh.height-5, 3)
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+visible {
		s.offset = s.cursor - visible + 1
	}

	for i := s.offset; i < len(s.found) && i < s.offset+visible; i++ {
		cursor := "  "
		if i == s.cursor {
			cursor = t.Cursor.Render("› ")
		}
		mark := t.Muted.Render("·")
		if s.selected[i] {
			mark = t.Success.Render("✓")
		}
		b.WriteString("  " + cursor + mark + " " + truncate(s.found[i].Rel, max(s.sh.width-8, 1)) + "\n")
	}
	b.WriteString("\n  " + t.Muted.Render(itoa(s.count())+" selected"))
	return b.String()
}
