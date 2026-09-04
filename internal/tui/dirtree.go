package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// dirTree is a directory tree: the hierarchy stays on screen, nodes expand and
// collapse in place, and the highlighted directory is the one that gets
// chosen. Only directories are shown, and the ones that are git working trees
// are marked.
type dirTree struct {
	theme Theme
	// chooseLabel is what enter does in the hosting screen's terms.
	chooseLabel string
	root        *treeNode
	nodes       []*treeNode // the currently visible rows, rebuilt on every change
	cursor      int
	offset      int
	height      int
	err         error
}

// treeNode is one directory in the tree.
type treeNode struct {
	path     string
	name     string
	depth    int
	isRepo   bool
	expanded bool
	loaded   bool
	children []*treeNode
	parent   *treeNode
}

func newDirTree(theme Theme, start, chooseLabel string) *dirTree {
	t := &dirTree{theme: theme, chooseLabel: chooseLabel, height: 12}
	t.reroot(start)
	return t
}

// reroot points the tree at a directory, expanding it so its contents show.
func (t *dirTree) reroot(dir string) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		if parent := filepath.Dir(dir); parent != dir {
			t.reroot(parent)
			return
		}
		t.err = err
		return
	}
	t.root = &treeNode{path: dir, name: shortenHome(dir), isRepo: isGitRepo(dir)}
	t.err = nil
	t.expand(t.root)
	t.cursor, t.offset = 0, 0
	t.rebuild()
}

// SetHeight tells the tree how many rows it may draw.
func (t *dirTree) SetHeight(height int) { t.height = max(height, 3) }

// expand loads a node's children the first time it is opened.
func (t *dirTree) expand(node *treeNode) {
	if !node.loaded {
		children, err := readDirNodes(node)
		if err != nil {
			// An unreadable directory stays collapsed rather than failing the
			// whole tree; a home directory routinely has a few.
			node.loaded = true
			return
		}
		node.children = children
		node.loaded = true
	}
	node.expanded = true
}

// rebuild flattens the expanded tree into the visible rows.
func (t *dirTree) rebuild() {
	t.nodes = t.nodes[:0]
	var walk func(node *treeNode)
	walk = func(node *treeNode) {
		t.nodes = append(t.nodes, node)
		if !node.expanded {
			return
		}
		for _, child := range node.children {
			walk(child)
		}
	}
	if t.root != nil {
		walk(t.root)
	}
	t.cursor = min(t.cursor, max(len(t.nodes)-1, 0))
}

// current is the highlighted node.
func (t *dirTree) current() *treeNode {
	if t.cursor < 0 || t.cursor >= len(t.nodes) {
		return nil
	}
	return t.nodes[t.cursor]
}

// update handles one key, returning the chosen directory once the tree is
// finished. An empty path with done set means it was cancelled.
func (t *dirTree) update(msg tea.KeyMsg) (chosen string, done bool) {
	node := t.current()

	switch msg.String() {
	case "esc", "ctrl+o":
		return "", true

	case "up", "k", "ctrl+p":
		t.cursor = max(t.cursor-1, 0)
	case "down", "j", "ctrl+n":
		t.cursor = min(t.cursor+1, max(len(t.nodes)-1, 0))
	case "home", "g":
		t.cursor = 0
	case "end", "G":
		t.cursor = max(len(t.nodes)-1, 0)
	case "pgup":
		t.cursor = max(t.cursor-t.height, 0)
	case "pgdown":
		t.cursor = min(t.cursor+t.height, max(len(t.nodes)-1, 0))

	case "right", "l", " ":
		if node == nil {
			break
		}
		if !node.expanded {
			t.expand(node)
			t.rebuild()
			break
		}
		// Already open: step into it.
		if len(node.children) > 0 {
			t.cursor++
		}

	case "left", "h":
		if node == nil {
			break
		}
		if node.expanded && len(node.children) > 0 {
			node.expanded = false
			t.rebuild()
			break
		}
		// Already closed: go out to the parent, or up out of the root.
		if node.parent != nil {
			t.selectNode(node.parent)
			break
		}
		t.up()

	case "-", "backspace":
		t.up()

	case "enter", "s":
		if node != nil {
			return node.path, true
		}
	}
	return "", false
}

// up re-roots the tree one level higher, keeping what is on screen expanded so
// the view grows outwards rather than starting over.
func (t *dirTree) up() {
	if t.root == nil {
		return
	}
	parent := filepath.Dir(t.root.path)
	if parent == t.root.path {
		return
	}

	previous := t.root
	t.reroot(parent)
	// Re-attach the branch we came from so going up never loses your place.
	for _, child := range t.root.children {
		if child.path == previous.path {
			child.children, child.loaded, child.expanded = previous.children, previous.loaded, previous.expanded
			reparent(child)
			t.rebuild()
			t.selectNode(child)
			return
		}
	}
}

// reparent fixes up the depths of a branch that has been moved under a new
// root, so the indentation still shows the real hierarchy.
func reparent(node *treeNode) {
	for _, child := range node.children {
		child.parent = node
		child.depth = node.depth + 1
		reparent(child)
	}
}

// selectNode puts the cursor on a node that is currently visible.
func (t *dirTree) selectNode(target *treeNode) {
	for i, node := range t.nodes {
		if node == target {
			t.cursor = i
			return
		}
	}
}

func (t *dirTree) hints() []Hint {
	return []Hint{
		{"↑/↓", "move"},
		{"→", "open"},
		{"←", "close"},
		{"-", "up a level"},
		{"enter", t.chooseLabel},
		{"esc", "cancel"},
	}
}

func (t *dirTree) view() string {
	theme := t.theme
	var b strings.Builder

	if t.err != nil {
		return "  " + theme.Failure.Render(t.err.Error())
	}

	t.scroll()
	for i := t.offset; i < len(t.nodes) && i < t.offset+t.height; i++ {
		node := t.nodes[i]

		cursor := "  "
		if i == t.cursor {
			cursor = theme.Cursor.Render("› ")
		}

		marker := "  "
		switch {
		case node.expanded && len(node.children) > 0:
			marker = "▾ "
		case node.loaded && len(node.children) == 0:
			marker = theme.Muted.Render("· ")
		default:
			marker = "▸ "
		}

		name := node.name
		if node.isRepo {
			name = theme.Success.Render(name)
		}
		line := cursor + strings.Repeat("  ", node.depth) + marker + name
		b.WriteString("  " + line + "\n")
	}

	if len(t.nodes) > t.height {
		b.WriteString("  " + theme.Muted.Render(itoa(t.cursor+1)+"/"+itoa(len(t.nodes))) + "\n")
	}
	if node := t.current(); node != nil {
		b.WriteString("  " + theme.Muted.Render(t.chooseLabel+": ") + shortenHome(node.path))
	}
	return b.String()
}

func (t *dirTree) scroll() {
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+t.height {
		t.offset = t.cursor - t.height + 1
	}
	if t.offset > max(len(t.nodes)-t.height, 0) {
		t.offset = max(len(t.nodes)-t.height, 0)
	}
}

// readDirNodes lists a directory's subdirectories as child nodes.
func readDirNodes(parent *treeNode) ([]*treeNode, error) {
	items, err := os.ReadDir(parent.path)
	if err != nil {
		return nil, err
	}

	var children []*treeNode
	for _, item := range items {
		name := item.Name()
		if strings.HasPrefix(name, ".") || !isDirEntry(parent.path, item) {
			continue
		}
		path := filepath.Join(parent.path, name)
		children = append(children, &treeNode{
			path:   path,
			name:   name,
			depth:  parent.depth + 1,
			isRepo: isGitRepo(path),
			parent: parent,
		})
	}
	sort.Slice(children, func(i, j int) bool { return children[i].name < children[j].name })
	return children, nil
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

// shortenHome writes a path the way a person would type it.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}

// isGitRepo reports whether a directory holds a .git entry.
func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
