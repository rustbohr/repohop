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

func TestDirBrowserNavigatesAndMarksRepos(t *testing.T) {
	root := completionTree(t)
	b := newDirBrowser(NewTheme(), root)

	if b.dir != root {
		t.Fatalf("browser opened on %q, want %q", b.dir, root)
	}
	view := b.view()
	if !strings.Contains(view, "workspace") {
		t.Errorf("browser does not list the subdirectories:\n%s", view)
	}

	// Move onto workspace and look inside: its one child is a git repository.
	b.selectName("workspace")
	if _, done := b.update(tea.KeyMsg{Type: tea.KeyRight}); done {
		t.Fatal("looking inside a directory finished the browser")
	}
	if filepath.Base(b.dir) != "workspace" {
		t.Fatalf("browser is in %q, want workspace", b.dir)
	}
	if got := b.entries; len(got) != 1 || got[0].name != "api" || !got[0].isRepo {
		t.Errorf("entries = %+v, want api marked as a repository", got)
	}

	// Left goes back up, keeping the cursor on where we came from.
	if _, done := b.update(tea.KeyMsg{Type: tea.KeyLeft}); done {
		t.Fatal("going up finished the browser")
	}
	if b.dir != root {
		t.Fatalf("browser is in %q, want back at the root", b.dir)
	}
	if entry, _ := b.current(); entry.name != "workspace" {
		t.Errorf("cursor is on %q, want the directory we came from", entry.name)
	}

	// Enter descends rather than choosing, so nothing is picked by accident on
	// the way to what you wanted.
	if chosen, done := b.update(tea.KeyMsg{Type: tea.KeyEnter}); done {
		t.Errorf("enter chose %q instead of opening the directory", chosen)
	}
	if filepath.Base(b.dir) != "workspace" {
		t.Fatalf("enter left the browser in %q, want inside workspace", b.dir)
	}

	// s chooses the directory being looked at.
	chosen, done := b.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if !done || chosen != filepath.Join(root, "workspace") {
		t.Errorf("s gave %q, %v, want the workspace path", chosen, done)
	}
}

func TestDirBrowserChoosesTheCurrentDirectory(t *testing.T) {
	root := completionTree(t)

	for _, key := range []string{"s", "."} {
		b := newDirBrowser(NewTheme(), root)
		chosen, done := b.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if !done || chosen != root {
			t.Errorf("%q gave %q, %v, want the current directory", key, chosen, done)
		}
	}
}

func TestPathInputBrowserRoundTrip(t *testing.T) {
	root := completionTree(t)
	p := newPathInput(NewTheme(), "~")
	p.SetValue(root)

	p.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if !p.browsing() {
		t.Fatal("ctrl+o did not open the browser")
	}
	if !strings.Contains(p.View(), "workspace") {
		t.Errorf("browsing view is not the browser:\n%s", p.View())
	}

	// Open sources, then choose it: the field takes the browsed path.
	p.browser.selectName("sources")
	p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if p.browsing() {
		t.Fatal("choosing a directory left the browser open")
	}
	if got := filepath.Base(p.Value()); got != "sources" {
		t.Errorf("value = %q, want the chosen directory", p.Value())
	}
}
