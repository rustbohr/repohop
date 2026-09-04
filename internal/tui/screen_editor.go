package tui

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rustbohr/repohop/internal/config"
	"github.com/rustbohr/repohop/internal/model"
)

// editorMode is which part of the editor has the keyboard.
type editorMode int

const (
	modeList editorMode = iota
	modeRename
	modeAddRepo
)

// editor changes an existing project: its name and which repositories are in
// it. The project's base directory is not edited here — repository entries are
// written relative to it when they sit underneath it and absolute when they do
// not, so the base never needs to change.
type editor struct {
	sh       *shared
	original model.Project

	mode   editorMode
	name   textinput.Model
	adding *dirTree

	repos  []model.Repo
	cursor int
	note   string
}

func newEditor(sh *shared, project model.Project) *editor {
	name := textinput.New()
	name.Prompt = "  "
	name.SetValue(project.Name)

	e := &editor{sh: sh, original: project, name: name}
	e.repos = append(e.repos, project.Repos...)
	return e
}

func (e *editor) Init() tea.Cmd { return nil }

func (e *editor) Title() string { return "edit " + e.original.Name }

func (e *editor) capturesKeys() bool { return e.mode != modeList }

func (e *editor) Hints() []Hint {
	switch e.mode {
	case modeRename:
		return []Hint{{"enter", "accept"}, {"esc", "discard"}}
	case modeAddRepo:
		return e.adding.hints()
	default:
		return []Hint{
			{"r", "rename"},
			{"a", "add repo"},
			{"d", "remove repo"},
			{"ctrl+s", "save"},
			{"esc", "discard"},
		}
	}
}

func (e *editor) Update(msg tea.Msg) (screen, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return e, nil
	}

	switch e.mode {
	case modeRename:
		switch key.String() {
		case "enter", "esc":
			if key.String() == "esc" {
				e.name.SetValue(e.original.Name)
			}
			e.mode = modeList
			e.name.Blur()
			return e, nil
		}
		var cmd tea.Cmd
		e.name, cmd = e.name.Update(key)
		return e, cmd

	case modeAddRepo:
		chosen, done := e.adding.update(key)
		switch {
		case !done:
			return e, nil
		case chosen == "":
			e.mode, e.adding = modeList, nil
			return e, nil
		default:
			return e, e.addRepo(chosen)
		}
	}

	return e.listKey(key)
}

func (e *editor) listKey(key tea.KeyMsg) (screen, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		e.cursor = max(e.cursor-1, 0)
	case "down", "j":
		e.cursor = min(e.cursor+1, max(len(e.repos)-1, 0))
	case "r":
		e.mode = modeRename
		e.name.Focus()
		e.name.CursorEnd()
		return e, textinput.Blink
	case "a":
		e.mode = modeAddRepo
		e.adding = newDirTree(e.sh.theme, config.ExpandPath(e.startingPath()), "add this repository")
		e.note = ""
		return e, nil
	case "d":
		if len(e.repos) > 0 {
			removed := e.repos[e.cursor]
			e.repos = append(e.repos[:e.cursor], e.repos[e.cursor+1:]...)
			e.cursor = min(e.cursor, max(len(e.repos)-1, 0))
			e.note = "removed " + removed.Name + " — not saved yet"
		}
	case "ctrl+s":
		return e, e.save()
	}
	return e, nil
}

// startingPath is where the add-a-repo field begins: the project's base, so
// the common case is a few keystrokes and a tab.
func (e *editor) startingPath() string {
	if e.original.Base != "" {
		return e.original.Base
	}
	if len(e.repos) > 0 {
		return shortenHome(filepath.Dir(e.repos[0].Path))
	}
	return "~"
}

// addRepo validates the chosen path and appends it to the list.
func (e *editor) addRepo(path string) tea.Cmd {
	for _, repo := range e.repos {
		if repo.Path == path {
			e.note = repo.Name + " is already in this project"
			return nil
		}
	}
	if err := e.sh.runner.Git.Check(e.sh.ctx, path); err != nil {
		// Not a hard stop: a repository that is missing right now is still a
		// legitimate config entry, and the dashboard will say so.
		e.note = shortenHome(path) + ": " + errLabel(err) + " — added anyway"
	} else {
		e.note = "added " + filepath.Base(path)
	}

	e.repos = append(e.repos, model.Repo{Name: filepath.Base(path), Path: path})
	e.cursor = len(e.repos) - 1
	e.mode, e.adding = modeList, nil
	return nil
}

// save writes the project back to the user's config file.
func (e *editor) save() tea.Cmd {
	name := strings.TrimSpace(e.name.Value())
	if name == "" {
		e.note = "the project needs a name"
		return nil
	}
	if len(e.repos) == 0 {
		e.note = "a project needs at least one repository"
		return nil
	}
	if name != e.original.Name {
		if _, taken := e.sh.cfg.Project(name); taken {
			e.note = "a project named " + name + " already exists"
			return nil
		}
	}

	spec := projectSpec(name, e.original.Base, e.repos)
	original := e.original
	sh := e.sh

	return func() tea.Msg {
		path := sh.cfg.UserPath
		if original.Name != name {
			if err := config.RenameProject(path, original.Name, name); err != nil {
				return errMsg{err}
			}
		}
		if err := config.AddProject(path, spec); err != nil {
			return errMsg{err}
		}
		return projectSavedMsg{previous: original.Name, name: name}
	}
}

// projectSpec renders repositories back into config entries: relative to the
// project's base where they sit underneath it, absolute where they do not, and
// with a display name only when it differs from the directory name.
func projectSpec(name, base string, repos []model.Repo) config.ProjectSpec {
	spec := config.ProjectSpec{Name: name, Base: base}
	root := ""
	if base != "" {
		root = config.ExpandPath(base)
	}

	for _, repo := range repos {
		entry := config.RepoSpec{Path: shortenHome(repo.Path)}
		if root != "" {
			if rel, err := filepath.Rel(root, repo.Path); err == nil && !strings.HasPrefix(rel, "..") {
				entry.Path = rel
			}
		}
		if repo.Name != filepath.Base(repo.Path) {
			entry.Name = repo.Name
		}
		spec.Repos = append(spec.Repos, entry)
	}
	return spec
}

func (e *editor) View() string {
	t := e.sh.theme
	if e.mode == modeAddRepo {
		e.adding.SetHeight(max(e.sh.height-6, 3))
		var b strings.Builder
		b.WriteString("\n  Add a repository to " + t.Title.Render(e.name.Value()) + "\n\n")
		b.WriteString(e.adding.view())
		return b.String()
	}

	var b strings.Builder
	b.WriteString("\n  " + t.ColumnHead.Render("NAME") + "  ")
	if e.mode == modeRename {
		b.WriteString(strings.TrimLeft(e.name.View(), " "))
	} else {
		b.WriteString(t.Title.Render(e.name.Value()))
	}
	b.WriteString("\n")
	if e.original.Base != "" {
		b.WriteString("  " + t.ColumnHead.Render("BASE") + "  " + t.Muted.Render(e.original.Base) + "\n")
	}
	b.WriteString("\n  " + t.ColumnHead.Render(strings.ToUpper(plural(len(e.repos), "repository"))) + "\n")

	names := make([]string, 0, len(e.repos))
	for _, repo := range e.repos {
		names = append(names, repo.Name)
	}
	nameWidth := columnWidth(names, 6, share(e.sh.width, 1, 3, 12))

	visible := max(e.sh.height-8, 3)
	start := max(min(e.cursor-visible+1, len(e.repos)-visible), 0)
	for i := start; i < len(e.repos) && i < start+visible; i++ {
		cursor := "  "
		if i == e.cursor && e.mode == modeList {
			cursor = t.Cursor.Render("› ")
		}
		line := cursor + cell(e.repos[i].Name, nameWidth) + "  " + t.Muted.Render(shortenHome(e.repos[i].Path))
		b.WriteString("  " + truncate(line, max(e.sh.width-2, 1)) + "\n")
	}

	if e.note != "" {
		b.WriteString("\n  " + t.Warning.Render(e.note))
	} else if e.changed() {
		b.WriteString("\n  " + t.Muted.Render("unsaved changes — ctrl+s to write "+shortenHome(e.sh.cfg.UserPath)))
	}
	return b.String()
}

// changed reports whether anything differs from the project as loaded.
func (e *editor) changed() bool {
	if strings.TrimSpace(e.name.Value()) != e.original.Name || len(e.repos) != len(e.original.Repos) {
		return true
	}
	for i, repo := range e.repos {
		if repo.Path != e.original.Repos[i].Path {
			return true
		}
	}
	return false
}
