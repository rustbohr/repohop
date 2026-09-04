package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// projectList is the first screen when more than one project is configured.
type projectList struct {
	sh     *shared
	cursor int
}

func newProjectList(sh *shared) *projectList { return &projectList{sh: sh} }

func (s *projectList) Init() tea.Cmd { return nil }

func (s *projectList) Title() string { return "projects" }

func (s *projectList) Hints() []Hint {
	if len(s.sh.cfg.Projects) == 0 {
		return []Hint{{"n", "set up a project"}, {"q", "quit"}}
	}
	return []Hint{
		{"↑/↓", "move"},
		{"enter", "open"},
		{"n", "new project"},
		{"q", "quit"},
	}
}

func (s *projectList) Update(msg tea.Msg) (screen, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	projects := s.sh.cfg.Projects

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
	case "enter":
		if len(projects) == 0 {
			return s, push(newSetup(s.sh))
		}
		project := projects[s.cursor]
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

	var b strings.Builder
	b.WriteString("\n")
	for i, project := range s.sh.cfg.Projects {
		cursor := "  "
		style := t.Row
		if i == s.cursor {
			cursor = t.Cursor.Render("› ")
			style = t.SelectedRow
		}
		line := style.Render(pad(project.Name, nameWidth)) + "  " +
			t.Muted.Render(pad(plural(len(project.Repos), "repo"), 10)) + "  " +
			t.Muted.Render(project.Source)
		b.WriteString(cursor + truncate(line, max(s.sh.width-2, 1)) + "\n")
	}
	return b.String()
}
