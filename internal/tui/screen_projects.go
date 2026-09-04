package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rustbohr/repohop/internal/config"
	"github.com/rustbohr/repohop/internal/model"
)

// projectList is the first screen when more than one project is configured.
type projectList struct {
	sh     *shared
	cursor int
	// confirm holds the project a delete is waiting on.
	confirm string
}

func newProjectList(sh *shared) *projectList {
	list := &projectList{sh: sh}

	// Start on the project that was open last, so the common case is enter.
	if state, err := config.LoadState(); err == nil && state.ActiveProject != "" {
		for i, project := range sh.cfg.Projects {
			if project.Name == state.ActiveProject {
				list.cursor = i
				break
			}
		}
	}
	return list
}

func (s *projectList) Init() tea.Cmd { return nil }

func (s *projectList) Title() string { return "projects" }

func (s *projectList) Hints() []Hint {
	if len(s.sh.cfg.Projects) == 0 {
		return []Hint{{"n", "set up a project"}, {"q", "quit"}}
	}
	if s.confirm != "" {
		return []Hint{{"y", "delete"}, {"n", "keep"}}
	}
	return []Hint{
		{"↑/↓", "move"},
		{"enter", "open"},
		{"n", "new"},
		{"e", "edit"},
		{"d", "delete"},
		{"q", "quit"},
	}
}

// clamp keeps the cursor inside the list, whatever has happened to the
// configuration since the last key.
func (s *projectList) clamp() {
	s.cursor = min(max(s.cursor, 0), max(len(s.sh.cfg.Projects)-1, 0))
}

// current is the highlighted project.
func (s *projectList) current() (model.Project, bool) {
	if s.cursor < 0 || s.cursor >= len(s.sh.cfg.Projects) {
		return model.Project{}, false
	}
	return s.sh.cfg.Projects[s.cursor], true
}

// editable reports whether a project lives in the user's own config file.
// Projects that come from a committed .repohop.yaml are shown but not edited
// here: rewriting a file the team shares is not this screen's business.
func (s *projectList) editable(project model.Project) bool {
	return project.Source == s.sh.cfg.UserPath
}

func (s *projectList) Update(msg tea.Msg) (screen, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}

	// The list is rebuilt from the config whenever it changes underneath —
	// deleting a project is the obvious case — so the cursor is re-checked on
	// every key rather than trusted from last time.
	s.clamp()
	projects := s.sh.cfg.Projects

	if s.confirm != "" {
		switch key.String() {
		case "y":
			name := s.confirm
			s.confirm = ""
			return s, func() tea.Msg {
				if err := config.RemoveProject(s.sh.cfg.UserPath, name); err != nil {
					return errMsg{err}
				}
				return projectDeletedMsg{name: name}
			}
		default:
			s.confirm = ""
		}
		return s, nil
	}

	switch key.String() {
	case "q":
		return s, tea.Quit
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(projects)-1 {
			s.cursor++
		}
	case "home", "g":
		s.cursor = 0
	case "end", "G":
		s.cursor = max(len(projects)-1, 0)
	case "n":
		return s, push(newSetup(s.sh))
	case "e":
		project, ok := s.current()
		if !ok {
			return s, nil
		}
		if !s.editable(project) {
			return s, flash("defined in " + shortenHome(project.Source) + " — edit that file")
		}
		return s, push(newEditor(s.sh, project))
	case "d":
		project, ok := s.current()
		if !ok {
			return s, nil
		}
		if !s.editable(project) {
			return s, flash("defined in " + shortenHome(project.Source) + " — remove it from that file")
		}
		s.confirm = project.Name
	case "enter":
		project, ok := s.current()
		if !ok {
			return s, push(newSetup(s.sh))
		}
		return s, func() tea.Msg { return projectChosenMsg{project} }
	}
	return s, nil
}

func (s *projectList) View() string {
	t := s.sh.theme
	if len(s.sh.cfg.Projects) == 0 {
		return strings.Join([]string{
			"",
			"  No projects configured yet.",
			"",
			"  " + t.Muted.Render("repohop drives a set of git repositories as one unit."),
			"  " + t.Muted.Render("Point it at yours and it will remember them."),
			"",
			"  " + t.Key.Render("n") + " " + t.Footer.Render("scan a directory and build a project"),
			"",
			"  " + t.Muted.Render("or write "+s.sh.cfg.UserPath+" by hand"),
		}, "\n")
	}

	names := make([]string, 0, len(s.sh.cfg.Projects))
	for _, project := range s.sh.cfg.Projects {
		names = append(names, project.Name)
	}
	nameWidth := columnWidth(names, 8, 32)

	s.clamp()

	var b strings.Builder
	b.WriteString("\n")
	if s.confirm != "" {
		b.WriteString("  " + t.Warning.Render("delete project "+s.confirm+"? the repositories on disk are not touched.") + "\n")
		b.WriteString("  " + t.Muted.Render("y to delete, any other key to keep") + "\n")
	}
	for i, project := range s.sh.cfg.Projects {
		cursor := "  "
		style := t.Row
		if i == s.cursor {
			cursor = t.Cursor.Render("› ")
			style = t.SelectedRow
		}
		// The read-only marker comes before the path, which is the part worth
		// losing when the terminal is narrow.
		marker := ""
		if !s.editable(project) {
			marker = t.Warning.Render("read-only here") + "  "
		}
		line := style.Render(cell(project.Name, nameWidth)) + "  " +
			t.Muted.Render(cell(plural(len(project.Repos), "repo"), 10)) + "  " +
			marker + t.Muted.Render(shortenHome(project.Source))
		b.WriteString(cursor + truncate(line, max(s.sh.width-2, 1)) + "\n")
	}
	return b.String()
}
