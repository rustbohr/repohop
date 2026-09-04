package ops

import (
	"path/filepath"
	"testing"

	"github.com/rustbohr/repohop/internal/model"
)

func TestSwitchOutcomes(t *testing.T) {
	origin := initOrigin(t, "feat/checkout")

	// A repository that carries the branch only on origin.
	remoteOnly := clone(t, origin, "api")
	// A repository that already has the branch checked out.
	already := clone(t, origin, "web")
	gitRun(t, already.Path, "checkout", "-q", "feat/checkout")
	// A repository that does not carry the branch at all.
	without := clone(t, initOrigin(t), "worker")
	// A repository with local modifications.
	dirty := clone(t, origin, "docs")
	writeFile(t, dirty.Path, "README.md", "work in progress\n")
	// A repository that has vanished from disk.
	missing := model.Repo{Name: "gone", Path: filepath.Join(t.TempDir(), "absent")}

	repos := []model.Repo{remoteOnly, already, without, dirty, missing}
	results := testRunner().Switch(ctx(t), repos, SwitchOptions{Branch: "feat/checkout", Pull: true}, nil)

	if len(results) != len(repos) {
		t.Fatalf("got %d results, want one per repository", len(results))
	}

	tests := []struct {
		repo    string
		outcome Outcome
		branch  string
	}{
		{"api", OutcomeSwitched, "feat/checkout"},
		{"web", OutcomeUnchanged, "feat/checkout"},
		{"worker", OutcomeNoBranch, "master"},
		{"docs", OutcomeSkippedDirty, "master"},
		{"gone", OutcomeFailed, ""},
	}
	for i, tt := range tests {
		got := results[i]
		if got.Repo.Name != tt.repo {
			t.Fatalf("result %d is for %q, want %q", i, got.Repo.Name, tt.repo)
		}
		if got.Outcome != tt.outcome {
			t.Errorf("%s: outcome = %v (%s), want %v", tt.repo, got.Outcome, got.Note, tt.outcome)
		}
		if tt.branch != "" && got.NewBranch != tt.branch {
			t.Errorf("%s: NewBranch = %q, want %q", tt.repo, got.NewBranch, tt.branch)
		}
	}

	// The summary's old-branch column reports where each repository started.
	if results[0].OldBranch != "master" {
		t.Errorf("api: OldBranch = %q, want master", results[0].OldBranch)
	}

	// A repository that lacks the branch is left exactly as it was, and one
	// with local changes is not touched either.
	if got := branchOf(t, without); got != "master" {
		t.Errorf("worker moved to %q; repohop must never create a branch", got)
	}
	if got := branchOf(t, dirty); got != "master" {
		t.Errorf("docs moved to %q despite being dirty", got)
	}

	// The origin-only branch became a local tracking branch.
	if got := branchOf(t, remoteOnly); got != "feat/checkout" {
		t.Fatalf("api is on %q, want feat/checkout", got)
	}
	if got := gitRun(t, remoteOnly.Path, "rev-parse", "--abbrev-ref", "feat/checkout@{upstream}"); got != "origin/feat/checkout" {
		t.Errorf("api upstream = %q, want origin/feat/checkout", got)
	}

	if n := SwitchFailures(results); n != 3 {
		t.Errorf("SwitchFailures() = %d, want 3 (no branch, dirty, missing)", n)
	}
}

func TestSwitchStashesWhenAsked(t *testing.T) {
	origin := initOrigin(t, "feat/checkout")
	repo := clone(t, origin, "api")
	writeFile(t, repo.Path, "README.md", "work in progress\n")

	results := testRunner().Switch(ctx(t), []model.Repo{repo},
		SwitchOptions{Branch: "feat/checkout", Pull: true, Dirty: DirtyStash}, nil)

	result := results[0]
	if result.Outcome != OutcomeSwitched {
		t.Fatalf("outcome = %v (%s), want switched", result.Outcome, result.Note)
	}
	if result.StashRef == "" {
		t.Error("StashRef is empty; the stash could not be offered for restore")
	}
	if got := branchOf(t, repo); got != "feat/checkout" {
		t.Errorf("repo is on %q, want feat/checkout", got)
	}
	if entries := gitRun(t, repo.Path, "stash", "list"); entries == "" {
		t.Error("no stash entry was created")
	}
}

func TestSwitchLocalOnlyBranchIsNotPulled(t *testing.T) {
	origin := initOrigin(t)
	repo := clone(t, origin, "api")
	gitRun(t, repo.Path, "branch", "local-experiment")

	results := testRunner().Switch(ctx(t), []model.Repo{repo},
		SwitchOptions{Branch: "local-experiment", Pull: true}, nil)

	result := results[0]
	if result.Outcome != OutcomeSwitched {
		t.Fatalf("outcome = %v, want switched", result.Outcome)
	}
	if result.Note != "local only, not pulled" {
		t.Errorf("Note = %q, want the local-only note", result.Note)
	}
}

func TestSwitchFastForwards(t *testing.T) {
	origin := initOrigin(t, "feat/checkout")
	repo := clone(t, origin, "api")

	// Someone pushes to the branch after we cloned.
	other := clone(t, origin, "other")
	gitRun(t, other.Path, "checkout", "-q", "feat/checkout")
	writeFile(t, other.Path, "later.txt", "newer\n")
	commit(t, other.Path, "later work")
	gitRun(t, other.Path, "push", "-q", "origin", "feat/checkout")

	runner := testRunner()
	runner.Fetch(ctx(t), []model.Repo{repo}, nil)
	results := runner.Switch(ctx(t), []model.Repo{repo}, SwitchOptions{Branch: "feat/checkout", Pull: true}, nil)

	if results[0].Outcome != OutcomeSwitched {
		t.Fatalf("outcome = %v (%s), want switched", results[0].Outcome, results[0].Note)
	}
	if _, err := statAt(repo.Path, "later.txt"); err != nil {
		t.Errorf("the pushed commit was not pulled: %v", err)
	}
}

func TestSwitchDivergedBranchIsReportedNotForced(t *testing.T) {
	origin := initOrigin(t, "feat/checkout")
	repo := clone(t, origin, "api")
	gitRun(t, repo.Path, "checkout", "-q", "feat/checkout")
	writeFile(t, repo.Path, "mine.txt", "local work\n")
	commit(t, repo.Path, "local work")

	other := clone(t, origin, "other")
	gitRun(t, other.Path, "checkout", "-q", "feat/checkout")
	writeFile(t, other.Path, "theirs.txt", "their work\n")
	commit(t, other.Path, "their work")
	gitRun(t, other.Path, "push", "-q", "origin", "feat/checkout")

	before := gitRun(t, repo.Path, "rev-parse", "HEAD")
	runner := testRunner()
	runner.Fetch(ctx(t), []model.Repo{repo}, nil)
	results := runner.Switch(ctx(t), []model.Repo{repo}, SwitchOptions{Branch: "feat/checkout", Pull: true}, nil)

	if results[0].Note != "not a fast-forward, not pulled" {
		t.Errorf("Note = %q, want the not-a-fast-forward note", results[0].Note)
	}
	if after := gitRun(t, repo.Path, "rev-parse", "HEAD"); after != before {
		t.Error("a divergent branch was moved; repohop must never force")
	}
}

func TestPreflight(t *testing.T) {
	origin := initOrigin(t, "feat/checkout")
	carrier := clone(t, origin, "api")
	dirty := clone(t, origin, "web")
	writeFile(t, dirty.Path, "README.md", "changed\n")
	without := clone(t, initOrigin(t), "worker")

	pre := testRunner().Preflight(ctx(t), []model.Repo{carrier, dirty, without}, "feat/checkout")

	if got := pre.Carriers(); got != 2 {
		t.Errorf("Carriers() = %d, want 2", got)
	}
	if got := pre.Dirty(); len(got) != 1 || got[0].Repo.Name != "web" {
		t.Errorf("Dirty() = %v, want only web", got)
	}
	if len(pre.States) != 3 {
		t.Fatalf("States has %d entries, want one per repository", len(pre.States))
	}
	if pre.Presence[2].Any() {
		t.Error("worker is reported as carrying a branch it does not have")
	}
}

func TestSequentialReportIsCalledPerRepository(t *testing.T) {
	origin := initOrigin(t, "feat/checkout")
	repos := []model.Repo{clone(t, origin, "api"), clone(t, origin, "web")}

	var seen []string
	testRunner().Switch(ctx(t), repos, SwitchOptions{Branch: "feat/checkout"}, func(result SwitchResult) {
		seen = append(seen, result.Repo.Name)
	})
	if len(seen) != 2 || seen[0] != "api" || seen[1] != "web" {
		t.Errorf("progress reported %v, want api then web in order", seen)
	}
}
