package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// completionTree builds a directory layout to complete against.
func completionTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"workspace", "scratch", "sources", "other", ".hidden", "workspace/api"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "workspace", "api", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDirTreeShowsTheHierarchy(t *testing.T) {
	root := completionTree(t)
	tree := newDirTree(NewTheme(), root, "choose")

	// The root opens expanded, so its subdirectories are visible immediately.
	if got := treeNames(tree); len(got) != 5 {
		t.Fatalf("visible rows = %v, want the root and its four subdirectories", got)
	}

	// Opening workspace keeps everything else on screen: the tree grows, it
	// does not replace the view.
	tree.selectName("workspace")
	if chosen, done := tree.update(keyOf("right")); done {
		t.Fatalf("right chose %q instead of opening the directory", chosen)
	}
	got := treeNames(tree)
	for _, want := range []string{"workspace", "api", "scratch", "sources", "other"} {
		if !contains(got, want) {
			t.Errorf("%q is not visible after expanding: %v", want, got)
		}
	}
	if indexOf(got, "api") != indexOf(got, "workspace")+1 {
		t.Errorf("api is not shown under its parent: %v", got)
	}

	// The child is indented and marked as a repository.
	view := tree.view()
	if !strings.Contains(view, "    ▸ api") && !strings.Contains(view, "    · api") {
		t.Errorf("child is not indented under its parent:\n%s", view)
	}

	// Left closes it again.
	tree.selectName("workspace")
	tree.update(keyOf("left"))
	if contains(treeNames(tree), "api") {
		t.Errorf("left did not collapse the directory: %v", treeNames(tree))
	}
}

func TestDirTreeChoosesTheHighlightedDirectory(t *testing.T) {
	root := completionTree(t)
	tree := newDirTree(NewTheme(), root, "choose")
	tree.selectName("sources")

	chosen, done := tree.update(keyOf("enter"))
	if !done || chosen != filepath.Join(root, "sources") {
		t.Errorf("enter gave %q, %v, want the sources path", chosen, done)
	}
}

func TestDirTreeGoesUpALevel(t *testing.T) {
	root := completionTree(t)
	tree := newDirTree(NewTheme(), filepath.Join(root, "workspace"), "choose")

	if _, done := tree.update(keyOf("-")); done {
		t.Fatal("going up finished the tree")
	}
	if tree.root.path != root {
		t.Fatalf("root is %q, want the parent %q", tree.root.path, root)
	}

	// The branch we came from is still open, with the cursor on it, so going
	// up never loses your place.
	names := treeNames(tree)
	if !contains(names, "api") {
		t.Errorf("the branch we came from was collapsed: %v", names)
	}
	if node := tree.current(); node == nil || node.name != "workspace" {
		t.Errorf("cursor is on %v, want workspace", node)
	}
}

func TestDirTreeLeftFromAClosedNodeGoesToTheParent(t *testing.T) {
	root := completionTree(t)
	tree := newDirTree(NewTheme(), root, "choose")
	tree.selectName("workspace")
	tree.update(keyOf("right"))
	tree.selectName("api")

	tree.update(keyOf("left"))
	if node := tree.current(); node == nil || node.name != "workspace" {
		t.Errorf("cursor is on %v, want its parent workspace", node)
	}
}

func TestDirTreeSkipsFilesAndHiddenDirectories(t *testing.T) {
	root := completionTree(t)
	tree := newDirTree(NewTheme(), root, "choose")

	names := treeNames(tree)
	if contains(names, "notes.txt") || contains(names, ".hidden") {
		t.Errorf("tree shows files or dot-directories: %v", names)
	}
}

// selectName puts the cursor on the first visible node with this name.
func (t *dirTree) selectName(name string) {
	for i, node := range t.nodes {
		if node.name == name || filepath.Base(node.path) == name {
			t.cursor = i
			return
		}
	}
}

func treeNames(t *dirTree) []string {
	names := make([]string, 0, len(t.nodes))
	for _, node := range t.nodes {
		names = append(names, filepath.Base(node.path))
	}
	return names
}

func keyOf(key string) tea.KeyMsg {
	switch key {
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

func contains(values []string, want string) bool {
	return indexOf(values, want) >= 0
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}

func TestDirTreeIndentationSurvivesGoingUp(t *testing.T) {
	root := completionTree(t)
	tree := newDirTree(NewTheme(), filepath.Join(root, "workspace"), "choose")

	// api is a child of workspace; after re-rooting one level up it must
	// still be drawn one level deeper than its parent.
	tree.selectName("workspace")
	tree.update(keyOf("right"))
	tree.update(keyOf("-"))

	var parent, child *treeNode
	for _, node := range tree.nodes {
		switch filepath.Base(node.path) {
		case "workspace":
			parent = node
		case "api":
			child = node
		}
	}
	if parent == nil || child == nil {
		t.Fatalf("expected both workspace and api to be visible: %v", treeNames(tree))
	}
	if child.depth != parent.depth+1 {
		t.Errorf("child depth = %d, parent depth = %d: the indentation no longer shows the hierarchy",
			child.depth, parent.depth)
	}
	if child.parent != parent {
		t.Error("the re-attached branch still points at its old parent")
	}
}

func TestShortenMiddleKeepsBothEnds(t *testing.T) {
	long := "/var/folders/df/djsxfhc17x95674wsm_g8s980000gn/T/TestProjects509516797/001/work/.repohop.yaml"
	got := shortenMiddle(long, 60)
	if len([]rune(got)) > 60 {
		t.Errorf("shortenMiddle produced %d cells, want at most 60: %q", len([]rune(got)), got)
	}
	if got[:8] != "/var/fol" {
		t.Errorf("the start of the path was lost: %q", got)
	}
	if !hasSuffix(got, ".repohop.yaml") {
		t.Errorf("the filename was lost, which is the useful end: %q", got)
	}
	if short := shortenMiddle("/tmp/x", 60); short != "/tmp/x" {
		t.Errorf("a path that fits was altered: %q", short)
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
