package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// dirBrowser is a small directory chooser: only directories are listed, and
// the ones that are git working trees are marked, which is exactly what both
// places that ask for a path care about.
type dirBrowser struct {
	theme   Theme
	dir     string
	entries []browserEntry
	cursor  int
	offset  int
	height  int
	err     error
}

type browserEntry struct {
	name   string
	isRepo bool
}

func newDirBrowser(theme Theme, start string) *dirBrowser {
	b := &dirBrowser{theme: theme, height: 10}
	b.open(start)
	return b
}

// open moves the browser to a directory, falling back to its parent when it
// cannot be read.
func (b *dirBrowser) open(dir string) {
	entries, err := readDirs(dir)
	if err != nil {
		if parent := filepath.Dir(dir); parent != dir {
			b.open(parent)
			return
		}
		b.err = err
		return
	}
	b.dir, b.entries, b.err = dir, entries, nil
	b.cursor, b.offset = 0, 0
}

// update handles one key, returning the chosen directory once the browser is
// finished. An empty path with done set means the browser was cancelled.
func (b *dirBrowser) update(msg tea.KeyMsg) (chosen string, done bool) {
	switch msg.String() {
	case "esc", "ctrl+o":
		return "", true
	case "up", "k", "ctrl+p":
		b.cursor = max(b.cursor-1, 0)
	case "down", "j", "ctrl+n":
		b.cursor = min(b.cursor+1, max(len(b.entries)-1, 0))
	case "home", "g":
		b.cursor = 0
	case "end", "G":
		b.cursor = max(len(b.entries)-1, 0)
	case "left", "h", "backspace":
		if parent := filepath.Dir(b.dir); parent != b.dir {
			previous := filepath.Base(b.dir)
			b.open(parent)
			b.selectName(previous)
		}
	case "right", "l", "tab":
		if entry, ok := b.current(); ok {
			b.open(filepath.Join(b.dir, entry.name))
		}
	case "enter":
		if entry, ok := b.current(); ok {
			return filepath.Join(b.dir, entry.name), true
		}
		// An empty directory is still a legitimate choice.
		return b.dir, true
	case ".":
		return b.dir, true
	}
	return "", false
}

func (b *dirBrowser) current() (browserEntry, bool) {
	if b.cursor < 0 || b.cursor >= len(b.entries) {
		return browserEntry{}, false
	}
	return b.entries[b.cursor], true
}

// selectName puts the cursor on a named entry, used when stepping back up.
func (b *dirBrowser) selectName(name string) {
	for i, entry := range b.entries {
		if entry.name == name {
			b.cursor = i
			return
		}
	}
}

func (b *dirBrowser) hints() []Hint {
	return []Hint{
		{"↑/↓", "move"},
		{"→", "look inside"},
		{"←", "up"},
		{"enter", "choose"},
		{".", "choose this directory"},
		{"esc", "type instead"},
	}
}

func (b *dirBrowser) view() string {
	t := b.theme
	var s strings.Builder
	s.WriteString("  " + t.Title.Render(shortenHome(b.dir)) + "\n\n")

	if b.err != nil {
		s.WriteString("  " + t.Failure.Render(b.err.Error()))
		return s.String()
	}
	if len(b.entries) == 0 {
		s.WriteString("  " + t.Muted.Render("no subdirectories — press . to choose this one"))
		return s.String()
	}

	b.scroll()
	for i := b.offset; i < len(b.entries) && i < b.offset+b.height; i++ {
		entry := b.entries[i]
		cursor := "  "
		if i == b.cursor {
			cursor = t.Cursor.Render("› ")
		}
		mark := "  "
		if entry.isRepo {
			mark = t.Success.Render("✓ ")
		}
		s.WriteString("  " + cursor + mark + entry.name + "\n")
	}
	if len(b.entries) > b.height {
		s.WriteString("  " + t.Muted.Render(itoa(b.cursor+1)+"/"+itoa(len(b.entries))))
	}
	return s.String()
}

func (b *dirBrowser) scroll() {
	if b.cursor < b.offset {
		b.offset = b.cursor
	}
	if b.cursor >= b.offset+b.height {
		b.offset = b.cursor - b.height + 1
	}
	if b.offset > max(len(b.entries)-b.height, 0) {
		b.offset = max(len(b.entries)-b.height, 0)
	}
}

// readDirs lists the subdirectories of dir, marking the git working trees.
func readDirs(dir string) ([]browserEntry, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var entries []browserEntry
	for _, item := range items {
		name := item.Name()
		if strings.HasPrefix(name, ".") || !isDirEntry(dir, item) {
			continue
		}
		_, err := os.Stat(filepath.Join(dir, name, ".git"))
		entries = append(entries, browserEntry{name: name, isRepo: err == nil})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries, nil
}
