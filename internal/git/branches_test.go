package git

import (
	"reflect"
	"testing"
)

func TestBranchesUnion(t *testing.T) {
	origin := initOrigin(t)

	// Publish a branch that only exists on the remote.
	seed := clone(t, origin)
	gitRun(t, seed, "checkout", "-q", "-b", "feat/remote-only")
	writeFile(t, seed, "f.txt", "x\n")
	commit(t, seed, "remote-only work")
	gitRun(t, seed, "push", "-q", "origin", "feat/remote-only")

	repo := clone(t, origin)
	gitRun(t, repo, "fetch", "-q", "origin")
	gitRun(t, repo, "checkout", "-q", "-b", "local-only")

	set, err := testRunner().Branches(ctx(t), repo, "origin")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"feat/remote-only", "local-only", "master"}
	if got := set.Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}

	tests := []struct {
		branch                string
		wantLocal, wantRemote bool
	}{
		{"master", true, true},
		{"local-only", true, false},
		{"feat/remote-only", false, true},
		{"nope", false, false},
		{"HEAD", false, false}, // origin/HEAD is not a branch
	}
	for _, tt := range tests {
		local, remote := set.Has(tt.branch)
		if local != tt.wantLocal || remote != tt.wantRemote {
			t.Errorf("Has(%q) = %v/%v, want %v/%v", tt.branch, local, remote, tt.wantLocal, tt.wantRemote)
		}
		if got := set.Any(tt.branch); got != (tt.wantLocal || tt.wantRemote) {
			t.Errorf("Any(%q) = %v", tt.branch, got)
		}
	}
}

func TestBranchesIgnoresOtherRemotes(t *testing.T) {
	origin := initOrigin(t)
	elsewhere := initOrigin(t)

	seed := clone(t, elsewhere)
	gitRun(t, seed, "checkout", "-q", "-b", "only-elsewhere")
	gitRun(t, seed, "push", "-q", "origin", "only-elsewhere")

	repo := clone(t, origin)
	gitRun(t, repo, "remote", "add", "elsewhere", elsewhere)
	gitRun(t, repo, "fetch", "-q", "elsewhere")

	set, err := testRunner().Branches(ctx(t), repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if set.Any("only-elsewhere") {
		t.Error("a branch on a non-origin remote leaked into the branch set")
	}
	if !set.Any("master") {
		t.Error("master missing from the branch set")
	}
}

func TestRefExists(t *testing.T) {
	repo := initRepo(t)
	gitRun(t, repo, "branch", "side")
	r := testRunner()

	tests := []struct {
		ref  string
		want bool
	}{
		{"refs/heads/master", true},
		{"refs/heads/side", true},
		{"refs/heads/absent", false},
		{"refs/remotes/origin/master", false},
	}
	for _, tt := range tests {
		if got := r.RefExists(ctx(t), repo, tt.ref); got != tt.want {
			t.Errorf("RefExists(%q) = %v, want %v", tt.ref, got, tt.want)
		}
	}
}
