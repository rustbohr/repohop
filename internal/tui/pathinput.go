package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rustbohr/repohop/internal/config"
)

// maxCompletions is how many candidate directories are listed under the field.
const maxCompletions = 8

// pathInput is a text field for a directory path: tab completes against the
// filesystem, and ctrl+o opens a browser for people who would rather look than
// type. It is a component, not a screen — setup and the project editor both
// use it.
type pathInput struct {
	input   textinput.Model
	theme   Theme
	browser *dirTree

	// matches are the directories the current fragment could become.
	matches []string
	// suggestion is what tab would append to the value.
	suggestion string

	// height is how many rows the directory tree may use.
	height int

	// While cycling, repeated tabs step through a frozen candidate list rather
	// than completing again, the way a shell does.
	cycling    bool
	cycleDir   string
	cycleNames []string
	cycleAt    int
}

func newPathInput(theme Theme, placeholder string) *pathInput {
	input := textinput.New()
	input.Prompt = "  "
	input.Placeholder = placeholder
	input.Focus()

	// textinput draws a suggestion inline, with the cursor sitting on the
	// first suggested character. Its own completion keys are unbound: what tab
	// does is decided here, so the ghost text always matches it.
	input.ShowSuggestions = true
	input.KeyMap.AcceptSuggestion = key.Binding{}
	input.KeyMap.NextSuggestion = key.Binding{}
	input.KeyMap.PrevSuggestion = key.Binding{}

	p := &pathInput{input: input, theme: theme}
	p.refresh()
	return p
}

// Value is the path as typed, unexpanded.
func (p *pathInput) Value() string { return strings.TrimSpace(p.input.Value()) }

// Path is the value expanded and made absolute.
func (p *pathInput) Path() string {
	value := p.Value()
	if value == "" {
		value = p.input.Placeholder
	}
	expanded := config.ExpandPath(value)
	if abs, err := filepath.Abs(expanded); err == nil {
		return abs
	}
	return expanded
}

func (p *pathInput) SetValue(value string) {
	p.input.SetValue(value)
	p.input.CursorEnd()
	p.cycling = false
	p.refresh()
}

// SetHeight tells the field how much room the directory tree has.
func (p *pathInput) SetHeight(height int) {
	p.height = height
	if p.browser != nil {
		p.browser.SetHeight(height)
	}
}

// browsing reports whether the directory tree is open.
func (p *pathInput) browsing() bool { return p.browser != nil }

// Update handles one key. It returns true when the key was consumed, so the
// screen can tell whether enter meant "submit" or was eaten by the browser.
func (p *pathInput) Update(msg tea.KeyMsg) (tea.Cmd, bool) {
	if p.browser != nil {
		chosen, done := p.browser.update(msg)
		if done {
			p.browser = nil
			if chosen != "" {
				p.SetValue(shortenHome(chosen))
			}
		}
		return nil, true
	}

	switch msg.String() {
	case "tab":
		p.complete()
		return nil, true
	case "ctrl+o":
		p.browser = newDirTree(p.theme, p.Path())
		if p.height > 0 {
			p.browser.SetHeight(p.height)
		}
		return nil, true
	case "enter":
		return nil, false // the screen decides what submitting means
	}

	p.cycling = false
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.refresh()
	return cmd, true
}

// complete applies the current suggestion; once there is no common prefix left
// to add, repeated tabs cycle through the candidates instead.
func (p *pathInput) complete() {
	if p.cycling && len(p.cycleNames) > 0 {
		p.cycleAt = (p.cycleAt + 1) % len(p.cycleNames)
		p.applyCycle()
		return
	}

	dir, _ := splitPath(p.input.Value())
	switch {
	case p.suggestion != "":
		p.SetValue(p.input.Value() + p.suggestion)
	case len(p.matches) > 1:
		p.cycling, p.cycleDir, p.cycleNames, p.cycleAt = true, dir, p.matches, 0
		p.applyCycle()
	}
}

// applyCycle writes the current candidate into the field without a trailing
// separator, so the next tab stays among the same siblings.
func (p *pathInput) applyCycle() {
	p.input.SetValue(p.cycleDir + p.cycleNames[p.cycleAt])
	p.input.CursorEnd()
	p.refresh()
	p.suggestion = "" // the candidate is already in the field in full
	p.showSuggestion()
}

// refresh recomputes the candidates for the current value.
func (p *pathInput) refresh() {
	dir, fragment := splitPath(p.input.Value())
	p.matches = matchingDirs(dir, fragment)
	p.suggestion = ""

	switch len(p.matches) {
	case 0:
	case 1:
		p.suggestion = strings.TrimPrefix(p.matches[0], fragment) + string(filepath.Separator)
	default:
		p.suggestion = strings.TrimPrefix(commonPrefix(p.matches), fragment)
	}
	p.showSuggestion()
}

// showSuggestion hands the current completion to the field for rendering.
func (p *pathInput) showSuggestion() {
	if p.suggestion == "" {
		p.input.SetSuggestions(nil)
		return
	}
	p.input.SetSuggestions([]string{p.input.Value() + p.suggestion})
}

func (p *pathInput) View() string {
	if p.browser != nil {
		return p.browser.view()
	}

	t := p.theme
	var b strings.Builder
	b.WriteString(p.input.View())
	if len(p.matches) > 1 {
		b.WriteString("\n\n  " + t.Muted.Render(strings.Join(truncateList(p.matches, maxCompletions), "  ")))
	}
	return b.String()
}

// Hints are the key reminders the hosting screen should show.
func (p *pathInput) Hints() []Hint {
	if p.browser != nil {
		return p.browser.hints()
	}
	hints := []Hint{{"tab", "complete"}, {"ctrl+o", "browse"}}
	return hints
}

// splitPath divides a typed path into the directory part (keeping its trailing
// separator) and the fragment being completed.
func splitPath(value string) (dir, fragment string) {
	value = strings.TrimLeft(value, " ")
	i := strings.LastIndexAny(value, `/\`)
	if i < 0 {
		return "", value
	}
	return value[:i+1], value[i+1:]
}

// matchingDirs lists the subdirectories of dir whose names start with
// fragment. An unreadable or missing directory simply has no candidates.
func matchingDirs(dir, fragment string) []string {
	lookup := dir
	if lookup == "" {
		lookup = "."
	}
	entries, err := os.ReadDir(config.ExpandPath(lookup))
	if err != nil {
		return nil
	}

	var matches []string
	for _, entry := range entries {
		name := entry.Name()
		if !isDirEntry(config.ExpandPath(lookup), entry) {
			continue
		}
		// Hidden directories stay out of the way until asked for by name.
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(fragment, ".") {
			continue
		}
		if strings.HasPrefix(name, fragment) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	return matches
}

// isDirEntry reports whether an entry is a directory, following symlinks.
func isDirEntry(dir string, entry os.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, entry.Name()))
	return err == nil && info.IsDir()
}

// commonPrefix is the longest prefix every candidate shares.
func commonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}

// truncateList caps a list for display, saying how many were left out.
func truncateList(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	out := append([]string{}, values[:limit]...)
	return append(out, "… +"+itoa(len(values)-limit))
}

// shortenHome writes a path back the way a person would type it.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || path == home {
		if path == home {
			return "~"
		}
		return path
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}
