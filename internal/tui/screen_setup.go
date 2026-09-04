package tui

import (
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
	sh    *shared
	step  setupStep
	input textinput.Model
	spin  spinner.Model

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
	input := textinput.New()
	input.Prompt = "  "
	input.Placeholder = "~/src"
	input.Focus()

	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	spin.Style = sh.theme.Spinner

	return &setup{sh: sh, input: input, spin: spin, selected: map[int]bool{}}
}

func (s *setup) Init() tea.Cmd { return textinput.Blink }

func (s *setup) Title() string { return "set up a project" }

func (s *setup) Hints() []Hint {
	switch s.step {
	case stepChoose:
		return []Hint{{"space", "select"}, {"a", "all"}, {"enter", "continue"}, {"esc", "back"}}
	case stepScanning:
		return []Hint{{"esc", "cancel"}}
	default:
		return []Hint{{"enter", "continue"}, {"esc", "cancel"}}
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
		if msg.String() == "enter" {
			root := strings.TrimSpace(s.input.Value())
			if root == "" {
				root = s.input.Placeholder
			}
			s.step = stepScanning
			return s, tea.Batch(s.spin.Tick, s.scan(config.ExpandPath(root)))
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return s, cmd

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
			s.input.SetValue(filepath.Base(s.root))
			s.input.Placeholder = "project name"
			s.input.CursorEnd()
			return s, textinput.Blink
		}
		return s, nil

	case stepName:
		if msg.String() == "enter" {
			return s, s.save()
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
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
	name := strings.TrimSpace(s.input.Value())
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
		if cfg, err := config.Load(config.Options{}); err == nil {
			*s.sh.cfg = *cfg
			if saved, ok := cfg.Project(name); ok {
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
		return strings.Join([]string{
			"",
			"  Which directory holds your repositories?",
			"  " + t.Muted.Render("repohop looks up to "+itoa(scan.DefaultDepth)+" levels below it."),
			"",
			s.input.View(),
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
			s.input.View(),
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
