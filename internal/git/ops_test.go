package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchPrunesDeletedBranch(t *testing.T) {
	origin := initOrigin(t)
	seed := clone(t, origin)
	gitRun(t, seed, "checkout", "-q", "-b", "temporary")
	gitRun(t, seed, "push", "-q", "origin", "temporary")

	repo := clone(t, origin)
	r := testRunner()
	if set, err := r.Branches(ctx(t), repo, "origin"); err != nil || !set.Any("temporary") {
		t.Fatalf("setup: temporary branch not visible (err=%v)", err)
	}

	gitRun(t, seed, "push", "-q", "origin", "--delete", "temporary")
	if err := r.Fetch(ctx(t), repo, "origin"); err != nil {
		t.Fatal(err)
	}

	set, err := r.Branches(ctx(t), repo, "origin")
	if err != nil {
		t.Fatal(err)
	}
	if set.Any("temporary") {
		t.Error("deleted remote branch survived a fetch; --prune is not taking effect")
	}
}

func TestFetchWithoutRemote(t *testing.T) {
	repo := initRepo(t)
	if err := testRunner().Fetch(ctx(t), repo, "origin"); !errors.Is(err, ErrNoRemote) {
		t.Fatalf("Fetch() = %v, want ErrNoRemote", err)
	}
}

func TestCheckout(t *testing.T) {
	origin := initOrigin(t)
	seed := clone(t, origin)
	gitRun(t, seed, "checkout", "-q", "-b", "feat/published")
	writeFile(t, seed, "f.txt", "x\n")
	commit(t, seed, "published work")
	gitRun(t, seed, "push", "-q", "origin", "feat/published")

	r := testRunner()

	t.Run("branch that exists only on origin is created as a tracking branch", func(t *testing.T) {
		repo := clone(t, origin)
		if err := r.Checkout(ctx(t), repo, "feat/published"); err != nil {
			t.Fatal(err)
		}
		st, err := r.Status(ctx(t), repo)
		if err != nil {
			t.Fatal(err)
		}
		if st.Branch != "feat/published" {
			t.Errorf("Branch = %q, want feat/published", st.Branch)
		}
		if st.Upstream != "origin/feat/published" {
			t.Errorf("Upstream = %q, want origin/feat/published", st.Upstream)
		}
	})

	t.Run("a branch that exists nowhere is never created", func(t *testing.T) {
		repo := clone(t, origin)
		err := r.Checkout(ctx(t), repo, "feat/imaginary")
		if err == nil {
			t.Fatal("Checkout() of a non-existent branch succeeded, want an error")
		}
		set, berr := r.Branches(ctx(t), repo, "origin")
		if berr != nil {
			t.Fatal(berr)
		}
		if set.Any("feat/imaginary") {
			t.Error("a failed checkout created the branch")
		}
	})
}

func TestSetUpstream(t *testing.T) {
	origin := initOrigin(t)
	repo := clone(t, origin)
	r := testRunner()

	gitRun(t, repo, "checkout", "-q", "-b", "feat/detached-upstream")
	writeFile(t, repo, "f.txt", "x\n")
	commit(t, repo, "work")
	gitRun(t, repo, "push", "-q", "--no-verify", "origin", "HEAD:feat/detached-upstream")
	gitRun(t, repo, "fetch", "-q", "origin")

	if st, err := r.Status(ctx(t), repo); err != nil || st.Upstream != "" {
		t.Fatalf("setup: upstream = %q (err=%v), want none", st.Upstream, err)
	}
	if err := r.SetUpstream(ctx(t), repo, "feat/detached-upstream", "origin"); err != nil {
		t.Fatal(err)
	}
	st, err := r.Status(ctx(t), repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.Upstream != "origin/feat/detached-upstream" {
		t.Errorf("Upstream = %q, want origin/feat/detached-upstream", st.Upstream)
	}
}

func TestPull(t *testing.T) {
	r := testRunner()

	t.Run("fast-forward", func(t *testing.T) {
		origin := initOrigin(t)
		repo := clone(t, origin)

		other := clone(t, origin)
		writeFile(t, other, "remote.txt", "theirs\n")
		commit(t, other, "remote commit")
		gitRun(t, other, "push", "-q", "origin", "master")

		if err := r.Pull(ctx(t), repo); err != nil {
			t.Fatal(err)
		}
		st, err := r.Status(ctx(t), repo)
		if err != nil {
			t.Fatal(err)
		}
		if st.Behind != 0 || st.Ahead != 0 {
			t.Errorf("after pull ahead/behind = %d/%d, want 0/0", st.Ahead, st.Behind)
		}
	})

	t.Run("divergent history is reported, never merged", func(t *testing.T) {
		origin := initOrigin(t)
		repo := clone(t, origin)

		other := clone(t, origin)
		writeFile(t, other, "remote.txt", "theirs\n")
		commit(t, other, "remote commit")
		gitRun(t, other, "push", "-q", "origin", "master")

		writeFile(t, repo, "local.txt", "mine\n")
		commit(t, repo, "local commit")

		before := gitRun(t, repo, "rev-parse", "HEAD")
		if err := r.Pull(ctx(t), repo); !errors.Is(err, ErrNotFastForward) {
			t.Fatalf("Pull() = %v, want ErrNotFastForward", err)
		}
		if after := gitRun(t, repo, "rev-parse", "HEAD"); after != before {
			t.Error("a refused pull moved HEAD")
		}
	})
}

func TestStashRoundTrip(t *testing.T) {
	repo := initRepo(t)
	r := testRunner()

	writeFile(t, repo, "README.md", "work in progress\n")
	writeFile(t, repo, "untracked.txt", "also mine\n")

	sha, err := r.Stash(ctx(t), repo, "repohop: switch")
	if err != nil {
		t.Fatal(err)
	}
	if sha == "" {
		t.Fatal("Stash() returned an empty id")
	}

	st, err := r.Status(ctx(t), repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.Dirty {
		t.Error("working tree still dirty after a stash")
	}

	// A second stash pushed on top must not confuse the restore.
	writeFile(t, repo, "README.md", "unrelated\n")
	if _, err := r.Stash(ctx(t), repo, "unrelated"); err != nil {
		t.Fatal(err)
	}

	if err := r.StashPop(ctx(t), repo, sha); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "work in progress\n" {
		t.Errorf("restored README = %q, want the stashed content", got)
	}
	if _, err := os.ReadFile(filepath.Join(repo, "untracked.txt")); err != nil {
		t.Errorf("untracked file was not restored: %v", err)
	}
}

func TestStashCleanTree(t *testing.T) {
	repo := initRepo(t)
	if _, err := testRunner().Stash(ctx(t), repo, "nothing"); !errors.Is(err, ErrNothingToStash) {
		t.Fatalf("Stash() = %v, want ErrNothingToStash", err)
	}
}

func TestStashPopMissingEntry(t *testing.T) {
	repo := initRepo(t)
	err := testRunner().StashPop(ctx(t), repo, "0000000000000000000000000000000000000000")
	if !errors.Is(err, ErrStashGone) {
		t.Fatalf("StashPop() = %v, want ErrStashGone", err)
	}
}

func TestErrorCommandIsReproducible(t *testing.T) {
	repo := initRepo(t)
	err := testRunner().Checkout(ctx(t), repo, "no-such-branch")
	var gerr *Error
	if !errors.As(err, &gerr) {
		t.Fatalf("Checkout() = %v, want a *git.Error", err)
	}
	if want := "git -C " + repo + " checkout no-such-branch --"; gerr.Command() != want {
		t.Errorf("Command() = %q, want %q", gerr.Command(), want)
	}
	if gerr.Stderr == "" {
		t.Error("Stderr is empty; the user would have nothing to go on")
	}
	if gerr.ExitCode <= 0 {
		t.Errorf("ExitCode = %d, want a real exit status", gerr.ExitCode)
	}
}
