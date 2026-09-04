package tui

import (
	"strings"
	"testing"

	"github.com/rustbohr/repohop/internal/git"
	"github.com/rustbohr/repohop/internal/model"
)

// pickerWith builds a picker over a fixed set of branches, bypassing the
// concurrent load.
func pickerWith(t *testing.T, repos []model.Repo, sets map[string]git.BranchSet) *picker {
	t.Helper()
	sh := &shared{theme: NewTheme(), width: 120, height: 20}
	p := newPicker(sh, repos)
	p.loading = false
	p.all = model.CollectBranches(repos, sets)
	p.filter()
	return p
}

func branchSet(local, remote []string) git.BranchSet {
	s := git.BranchSet{Local: map[string]struct{}{}, Remote: map[string]struct{}{}}
	for _, n := range local {
		s.Local[n] = struct{}{}
	}
	for _, n := range remote {
		s.Remote[n] = struct{}{}
	}
	return s
}

func names(p *picker) []string {
	out := make([]string, 0, len(p.matches))
	for _, m := range p.matches {
		out = append(out, m.info.Name)
	}
	return out
}

func testPicker(t *testing.T) *picker {
	repos := []model.Repo{{Name: "api", Path: "/r/api"}, {Name: "web", Path: "/r/web"}, {Name: "worker", Path: "/r/worker"}}
	return pickerWith(t, repos, map[string]git.BranchSet{
		"/r/api":    branchSet([]string{"master", "feat/checkout"}, []string{"master", "feat/checkout", "feat/checkout-v2"}),
		"/r/web":    branchSet([]string{"master"}, []string{"master", "feat/checkout"}),
		"/r/worker": branchSet([]string{"master", "chore/tidy"}, []string{"master"}),
	})
}

func TestPickerUnfilteredOrder(t *testing.T) {
	p := testPicker(t)
	want := []string{"master", "feat/checkout", "chore/tidy", "feat/checkout-v2"}
	if got := names(p); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v (most widely carried first, then alphabetical)", got, want)
	}
}

func TestPickerFilters(t *testing.T) {
	p := testPicker(t)
	p.query = "chk"
	p.filter()

	got := names(p)
	if len(got) < 2 {
		t.Fatalf("fuzzy filter returned %v, want the checkout branches", got)
	}
	for _, name := range got {
		if !strings.Contains(name, "checkout") {
			t.Errorf("filter matched %q, want only the checkout branches", name)
		}
	}
	if len(p.matches[0].positions) == 0 {
		t.Error("no matched positions recorded; the list cannot highlight the match")
	}
}

func TestPickerExactMatchWinsOverScore(t *testing.T) {
	p := testPicker(t)
	p.query = "feat/checkout"
	p.filter()

	if got := names(p); got[0] != "feat/checkout" {
		t.Errorf("first match = %q, want the exact match feat/checkout", got[0])
	}
}

func TestPickerBreaksTiesByRepoCount(t *testing.T) {
	repos := []model.Repo{{Name: "api", Path: "/r/api"}, {Name: "web", Path: "/r/web"}}
	p := pickerWith(t, repos, map[string]git.BranchSet{
		// Two branches whose names score identically for the query "release";
		// one exists in both repos, the other in one.
		"/r/api": branchSet([]string{"release-a", "release-b"}, nil),
		"/r/web": branchSet([]string{"release-b"}, nil),
	})
	p.query = "release"
	p.filter()

	if got := names(p); got[0] != "release-b" {
		t.Errorf("first match = %q, want release-b: a branch every repo carries", got[0])
	}
}

func TestPickerNoMatches(t *testing.T) {
	p := testPicker(t)
	p.query = "zzzz"
	p.filter()

	if len(p.matches) != 0 {
		t.Fatalf("matches = %v, want none", names(p))
	}
	if view := p.View(); !strings.Contains(view, "no branch matches") {
		t.Errorf("view does not say the filter matched nothing:\n%s", view)
	}
}

func TestPickerLayoutDegradesWithWidth(t *testing.T) {
	p := testPicker(t)
	tests := []struct {
		width int
		want  layout
	}{
		{140, layoutSide},
		{100, layoutSide},
		{80, layoutBelow},
		{60, layoutBelow},
		{50, layoutSummary},
	}
	for _, tt := range tests {
		p.sh.width = tt.width
		if got := p.layout(); got != tt.want {
			t.Errorf("width %d: layout = %v, want %v", tt.width, got, tt.want)
		}
		if view := p.View(); view == "" {
			t.Errorf("width %d: empty view", tt.width)
		}
	}

	// The narrowest layout still says how many repositories carry the branch.
	p.sh.width = 50
	if view := p.View(); !strings.Contains(view, "3/3 repos") {
		t.Errorf("narrow view lost the summary line:\n%s", view)
	}
}

func TestPickerPreviewShowsWhereBranchesLive(t *testing.T) {
	p := testPicker(t)
	p.cursor = 1 // feat/checkout
	view := p.renderPreview(60)

	for _, want := range []string{"feat/checkout", "api", "local origin", "web", "origin", "worker", "—"} {
		if !strings.Contains(view, want) {
			t.Errorf("preview is missing %q:\n%s", want, view)
		}
	}
}

func TestPickerCursorStaysInRange(t *testing.T) {
	p := testPicker(t)
	p.move(100)
	if p.cursor != len(p.matches)-1 {
		t.Errorf("cursor = %d, want the last row %d", p.cursor, len(p.matches)-1)
	}
	p.move(-100)
	if p.cursor != 0 {
		t.Errorf("cursor = %d, want 0", p.cursor)
	}

	// Re-filtering resets the cursor so it cannot point past the new list.
	p.cursor = 3
	p.query = "master"
	p.filter()
	if p.cursor != 0 {
		t.Errorf("cursor = %d after filtering, want 0", p.cursor)
	}
}
