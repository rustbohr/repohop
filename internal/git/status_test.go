package git

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestCheck(t *testing.T) {
	repo := initRepo(t)
	plain := t.TempDir()
	r := testRunner()

	tests := []struct {
		name string
		dir  string
		want error
	}{
		{"working tree", repo, nil},
		{"directory that is not a repo", plain, ErrNotRepo},
		{"path that does not exist", filepath.Join(plain, "nope"), ErrMissing},
		{"file, not a directory", filepath.Join(repo, "README.md"), ErrMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Check(ctx(t), tt.dir); !errors.Is(got, tt.want) {
				t.Fatalf("Check() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatusClean(t *testing.T) {
	repo := initRepo(t)
	st, err := testRunner().Status(ctx(t), repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "master" {
		t.Errorf("Branch = %q, want master", st.Branch)
	}
	if st.Dirty {
		t.Error("Dirty = true, want false")
	}
	if st.Detached || st.Unborn {
		t.Errorf("Detached = %v, Unborn = %v, want both false", st.Detached, st.Unborn)
	}
	if len(st.Head) != 7 {
		t.Errorf("Head = %q, want a 7-character short sha", st.Head)
	}
	if st.Upstream != "" {
		t.Errorf("Upstream = %q, want empty", st.Upstream)
	}
	if st.LastCommit.IsZero() {
		t.Error("LastCommit is zero")
	}
	if st.Ref() != "master" {
		t.Errorf("Ref() = %q, want master", st.Ref())
	}
}

func TestStatusDirtyIgnoresUntracked(t *testing.T) {
	r := testRunner()

	t.Run("untracked file does not count as dirty", func(t *testing.T) {
		repo := initRepo(t)
		writeFile(t, repo, "scratch.txt", "new\n")
		st, err := r.Status(ctx(t), repo)
		if err != nil {
			t.Fatal(err)
		}
		if st.Dirty {
			t.Error("Dirty = true for an untracked file, want false")
		}
	})

	t.Run("modified tracked file is dirty", func(t *testing.T) {
		repo := initRepo(t)
		writeFile(t, repo, "README.md", "changed\n")
		st, err := r.Status(ctx(t), repo)
		if err != nil {
			t.Fatal(err)
		}
		if !st.Dirty {
			t.Error("Dirty = false for a modified tracked file, want true")
		}
	})

	t.Run("staged change is dirty", func(t *testing.T) {
		repo := initRepo(t)
		writeFile(t, repo, "README.md", "staged\n")
		gitRun(t, repo, "add", "README.md")
		st, err := r.Status(ctx(t), repo)
		if err != nil {
			t.Fatal(err)
		}
		if !st.Dirty {
			t.Error("Dirty = false for a staged change, want true")
		}
	})
}

func TestStatusUpstreamAheadBehind(t *testing.T) {
	origin := initOrigin(t)
	repo := clone(t, origin)

	// Someone else pushes a commit after we cloned.
	other := clone(t, origin)
	writeFile(t, other, "remote.txt", "from elsewhere\n")
	commit(t, other, "remote commit")
	gitRun(t, other, "push", "-q", "origin", "master")

	writeFile(t, repo, "local.txt", "mine\n")
	commit(t, repo, "local commit")
	gitRun(t, repo, "fetch", "-q", "origin")

	st, err := testRunner().Status(ctx(t), repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.Upstream != "origin/master" {
		t.Errorf("Upstream = %q, want origin/master", st.Upstream)
	}
	if st.Ahead != 1 || st.Behind != 1 {
		t.Errorf("ahead/behind = %d/%d, want 1/1", st.Ahead, st.Behind)
	}
}

func TestStatusDetached(t *testing.T) {
	repo := initRepo(t)
	gitRun(t, repo, "checkout", "-q", "--detach", "HEAD")

	st, err := testRunner().Status(ctx(t), repo)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Detached {
		t.Fatal("Detached = false, want true")
	}
	if st.Branch != "" {
		t.Errorf("Branch = %q, want empty when detached", st.Branch)
	}
	if want := "(detached @ " + st.Head + ")"; st.Ref() != want {
		t.Errorf("Ref() = %q, want %q", st.Ref(), want)
	}
}

func TestStatusUnborn(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "-c", "init.defaultBranch=master", "init", "-q", ".")

	st, err := testRunner().Status(ctx(t), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Unborn {
		t.Error("Unborn = false, want true")
	}
	if !st.LastCommit.IsZero() {
		t.Error("LastCommit set on a repo with no commits")
	}
	if st.Ref() != "(unborn)" {
		t.Errorf("Ref() = %q, want (unborn)", st.Ref())
	}
}

func TestParseAheadBehind(t *testing.T) {
	tests := []struct {
		in            string
		ahead, behind int
	}{
		{"+0 -0", 0, 0},
		{"+3 -1", 3, 1},
		{"+12 -0", 12, 0},
		{"", 0, 0},
		{"garbage", 0, 0},
	}
	for _, tt := range tests {
		ahead, behind := parseAheadBehind(tt.in)
		if ahead != tt.ahead || behind != tt.behind {
			t.Errorf("parseAheadBehind(%q) = %d/%d, want %d/%d", tt.in, ahead, behind, tt.ahead, tt.behind)
		}
	}
}
