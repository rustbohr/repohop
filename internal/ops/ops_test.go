package ops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rustbohr/repohop/internal/git"
	"github.com/rustbohr/repohop/internal/model"
)

func statAt(dir, name string) (os.FileInfo, error) { return os.Stat(filepath.Join(dir, name)) }

func TestStatusKeepsOrderAndReportsBrokenRepos(t *testing.T) {
	origin := initOrigin(t)
	good := clone(t, origin, "api")
	notARepo := model.Repo{Name: "web", Path: t.TempDir()}
	missing := model.Repo{Name: "worker", Path: filepath.Join(t.TempDir(), "absent")}

	states := testRunner().Status(ctx(t), []model.Repo{good, notARepo, missing})

	if len(states) != 3 {
		t.Fatalf("got %d states, want one per repository", len(states))
	}
	for i, want := range []string{"api", "web", "worker"} {
		if states[i].Repo.Name != want {
			t.Errorf("state %d is for %q, want %q (project order must be preserved)", i, states[i].Repo.Name, want)
		}
	}
	if !states[0].OK() || states[0].Status.Branch != "master" {
		t.Errorf("api = %+v, want a clean read of master", states[0])
	}
	if states[1].Err != git.ErrNotRepo {
		t.Errorf("web error = %v, want ErrNotRepo", states[1].Err)
	}
	if states[2].Err != git.ErrMissing {
		t.Errorf("worker error = %v, want ErrMissing", states[2].Err)
	}
}

func TestBranchesAcrossRepos(t *testing.T) {
	origin := initOrigin(t, "feat/checkout")
	api := clone(t, origin, "api")
	worker := clone(t, initOrigin(t), "worker")
	broken := model.Repo{Name: "gone", Path: filepath.Join(t.TempDir(), "absent")}

	sets, failures := testRunner().Branches(ctx(t), []model.Repo{api, worker, broken})

	if len(failures) != 1 || failures[0].Repo.Name != "gone" {
		t.Errorf("failures = %v, want only the missing repository", failures)
	}
	if !sets[api.Path].Any("feat/checkout") {
		t.Error("api does not report feat/checkout")
	}
	if sets[worker.Path].Any("feat/checkout") {
		t.Error("worker reports a branch it does not have")
	}

	infos := model.CollectBranches([]model.Repo{api, worker}, sets)
	if len(infos) != 2 || infos[0].Name != "master" {
		t.Fatalf("CollectBranches() = %+v, want master first", infos)
	}
	if infos[1].Name != "feat/checkout" || infos[1].Count() != 1 {
		t.Errorf("second candidate = %q in %d repos, want feat/checkout in 1", infos[1].Name, infos[1].Count())
	}
}

func TestFetchNotes(t *testing.T) {
	origin := initOrigin(t)
	withRemote := clone(t, origin, "api")

	standalone := model.Repo{Name: "solo", Path: filepath.Join(t.TempDir(), "solo")}
	if err := os.MkdirAll(standalone.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, standalone.Path, "-c", "init.defaultBranch=master", "init", "-q", ".")

	results := testRunner().Fetch(ctx(t), []model.Repo{withRemote, standalone})

	if !results[0].OK() || results[0].Note != "" {
		t.Errorf("api = %+v, want a clean fetch", results[0])
	}
	if !results[1].OK() || results[1].Note != "no remote configured" {
		t.Errorf("solo = %+v, want the no-remote note rather than a failure", results[1])
	}
	if n := Failures(results); n != 0 {
		t.Errorf("Failures() = %d, want 0", n)
	}
}

func TestPullNotes(t *testing.T) {
	origin := initOrigin(t)
	behind := clone(t, origin, "api")
	other := clone(t, origin, "other")
	writeFile(t, other.Path, "later.txt", "newer\n")
	commit(t, other.Path, "later work")
	gitRun(t, other.Path, "push", "-q", "origin", "master")

	detached := clone(t, origin, "web")
	gitRun(t, detached.Path, "checkout", "-q", "--detach", "HEAD")

	noUpstream := clone(t, origin, "worker")
	gitRun(t, noUpstream.Path, "checkout", "-q", "-b", "local-only")

	results := testRunner().Pull(ctx(t), []model.Repo{behind, detached, noUpstream}, nil)

	if !results[0].OK() || results[0].Note != "" {
		t.Errorf("api = %+v, want a clean pull", results[0])
	}
	if _, err := statAt(behind.Path, "later.txt"); err != nil {
		t.Errorf("api was not fast-forwarded: %v", err)
	}
	if results[1].Note != "detached HEAD, not pulled" {
		t.Errorf("web note = %q, want the detached-HEAD note", results[1].Note)
	}
	if results[2].Note != "no upstream, not pulled" {
		t.Errorf("worker note = %q, want the no-upstream note", results[2].Note)
	}
}
