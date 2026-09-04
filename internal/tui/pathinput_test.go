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

func typeInto(p *pathInput, s string) {
	for _, r := range s {
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func pressKey(p *pathInput, key tea.KeyType) {
	p.Update(tea.KeyMsg{Type: key})
}

func TestSplitPath(t *testing.T) {
	tests := []struct{ in, dir, fragment string }{
		{"", "", ""},
		{"ms", "", "ms"},
		{"~/", "~/", ""},
		{"~/ms", "~/", "ms"},
		{"/home/x/src/ap", "/home/x/src/", "ap"},
		{"/", "/", ""},
	}
	for _, tt := range tests {
		dir, fragment := splitPath(tt.in)
		if dir != tt.dir || fragment != tt.fragment {
			t.Errorf("splitPath(%q) = %q, %q, want %q, %q", tt.in, dir, fragment, tt.dir, tt.fragment)
		}
	}
}

func TestCommonPrefix(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"workspace"}, "workspace"},
		{[]string{"workspace", "scratch"}, "scra"},
		{[]string{"workspace", "sources"}, "s"},
		{[]string{"api", "web"}, ""},
	}
	for _, tt := range tests {
		if got := commonPrefix(tt.in); got != tt.want {
			t.Errorf("commonPrefix(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMatchingDirsSkipsFilesAndHiddenDirs(t *testing.T) {
	root := completionTree(t)

	got := matchingDirs(root+string(filepath.Separator), "")
	want := []string{"other", "workspace", "scratch", "sources"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("matchingDirs = %v, want %v (no files, no dot-directories)", got, want)
	}

	// A fragment that starts with a dot asks for the hidden ones by name.
	if got := matchingDirs(root+string(filepath.Separator), "."); len(got) != 1 || got[0] != ".hidden" {
		t.Errorf("matchingDirs(\".\") = %v, want [.hidden]", got)
	}
}

func TestPathInputCompletesToTheCommonPrefix(t *testing.T) {
	root := completionTree(t)
	p := newPathInput(NewTheme(), "~")

	typeInto(p, root+string(filepath.Separator)+"scr")
	if p.suggestion != "a" {
		t.Errorf("suggestion = %q, want %q (workspace and scratch share scra)", p.suggestion, "a")
	}

	pressKey(p, tea.KeyTab)
	if got := filepath.Base(p.Value()); got != "scra" {
		t.Errorf("after tab the value ends in %q, want scra", got)
	}

	// One more character leaves a single candidate, which completes fully and
	// adds the separator so the next level can be typed straight away.
	typeInto(p, "p")
	pressKey(p, tea.KeyTab)
	if got := p.Value(); !strings.HasSuffix(got, "workspace"+string(filepath.Separator)) {
		t.Errorf("value = %q, want it completed to workspace/", got)
	}
	if got := filepath.Base(p.Path()); got != "workspace" {
		t.Errorf("Path() = %q, want it to resolve to the workspace directory", p.Path())
	}
}

func TestPathInputCyclesWhenThereIsNoCommonPrefix(t *testing.T) {
	root := completionTree(t)
	p := newPathInput(NewTheme(), "~")
	typeInto(p, root+string(filepath.Separator)+"scra")

	pressKey(p, tea.KeyTab) // no common prefix left: cycles to the first match
	first := p.Value()
	pressKey(p, tea.KeyTab)
	second := p.Value()

	if first == second {
		t.Fatalf("tab did not cycle: %q twice", first)
	}
	for _, value := range []string{first, second} {
		base := filepath.Base(strings.TrimSuffix(value, string(filepath.Separator)))
		if base != "workspace" && base != "scratch" {
			t.Errorf("cycled to %q, want one of the scra* directories", base)
		}
	}
}

func TestPathInputShowsCandidatesAndGhostText(t *testing.T) {
	root := completionTree(t)
	p := newPathInput(NewTheme(), "~")
	typeInto(p, root+string(filepath.Separator)+"s")

	view := p.View()
	for _, want := range []string{"workspace", "scratch", "sources"} {
		if !strings.Contains(view, want) {
			t.Errorf("view does not offer %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "other") {
		t.Errorf("view offers a directory that does not match:\n%s", view)
	}
}

func TestPathInputHasNoCandidatesForAMissingDirectory(t *testing.T) {
	p := newPathInput(NewTheme(), "~")
	typeInto(p, filepath.Join(t.TempDir(), "nowhere")+string(filepath.Separator))

	if len(p.matches) != 0 || p.suggestion != "" {
		t.Errorf("matches = %v, suggestion = %q, want neither", p.matches, p.suggestion)
	}
	if p.View() == "" {
		t.Error("empty view for an unreadable directory")
	}
}

func TestDirTreeShowsTheHierarchy(t *testing.T) {
	root := completionTree(t)
	tree := newDirTree(NewTheme(), root)

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
	tree := newDirTree(NewTheme(), root)
	tree.selectName("sources")

	chosen, done := tree.update(keyOf("enter"))
	if !done || chosen != filepath.Join(root, "sources") {
		t.Errorf("enter gave %q, %v, want the sources path", chosen, done)
	}
}

func TestDirTreeGoesUpALevel(t *testing.T) {
	root := completionTree(t)
	tree := newDirTree(NewTheme(), filepath.Join(root, "workspace"))

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
	tree := newDirTree(NewTheme(), root)
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
	tree := newDirTree(NewTheme(), root)

	names := treeNames(tree)
	if contains(names, "notes.txt") || contains(names, ".hidden") {
		t.Errorf("tree shows files or dot-directories: %v", names)
	}
}

func TestPathInputTreeRoundTrip(t *testing.T) {
	root := completionTree(t)
	p := newPathInput(NewTheme(), "~")
	p.SetValue(root)

	p.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if !p.browsing() {
		t.Fatal("ctrl+o did not open the tree")
	}
	if !strings.Contains(p.View(), "workspace") {
		t.Errorf("the tree is not showing:\n%s", p.View())
	}

	p.browser.selectName("sources")
	p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p.browsing() {
		t.Fatal("choosing a directory left the tree open")
	}
	if got := filepath.Base(p.Value()); got != "sources" {
		t.Errorf("value = %q, want the chosen directory", p.Value())
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
	tree := newDirTree(NewTheme(), filepath.Join(root, "workspace"))

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
