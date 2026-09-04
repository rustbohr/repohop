package model

import (
	"testing"
	"time"

	"github.com/rustbohr/repohop/internal/git"
)

func set(local, remote []string) git.BranchSet {
	s := git.BranchSet{Local: map[string]struct{}{}, Remote: map[string]struct{}{}}
	for _, n := range local {
		s.Local[n] = struct{}{}
	}
	for _, n := range remote {
		s.Remote[n] = struct{}{}
	}
	return s
}

func TestCollectBranches(t *testing.T) {
	repos := []Repo{{Name: "api", Path: "/r/api"}, {Name: "web", Path: "/r/web"}, {Name: "worker", Path: "/r/worker"}}
	sets := map[string]git.BranchSet{
		"/r/api":    set([]string{"master", "feat/checkout"}, []string{"master", "feat/checkout"}),
		"/r/web":    set([]string{"master"}, []string{"master", "feat/checkout"}),
		"/r/worker": set([]string{"master", "aaa-lonely"}, []string{"master"}),
	}

	infos := CollectBranches(repos, sets)

	// Most widely carried first, then alphabetical; "aaa-lonely" sorts first
	// alphabetically but is in one repo only, so it comes last.
	want := []string{"master", "feat/checkout", "aaa-lonely"}
	if len(infos) != len(want) {
		t.Fatalf("got %d branches, want %d", len(infos), len(want))
	}
	for i, name := range want {
		if infos[i].Name != name {
			t.Errorf("branch %d = %q, want %q", i, infos[i].Name, name)
		}
	}

	if got := infos[0].Count(); got != 3 {
		t.Errorf("master carried by %d repos, want 3", got)
	}
	if got := infos[1].Count(); got != 2 {
		t.Errorf("feat/checkout carried by %d repos, want 2", got)
	}

	// Every candidate carries an entry per repo, in project order, including
	// the repos that do not have the branch.
	feature := infos[1]
	if len(feature.In) != 3 {
		t.Fatalf("feat/checkout has %d presences, want one per repo", len(feature.In))
	}
	tests := []struct {
		repo  string
		where string
	}{
		{"api", "local origin"},
		{"web", "origin"},
		{"worker", "—"},
	}
	for i, tt := range tests {
		if feature.In[i].Repo.Name != tt.repo {
			t.Fatalf("presence %d is for %q, want %q", i, feature.In[i].Repo.Name, tt.repo)
		}
		if got := feature.In[i].Where(); got != tt.where {
			t.Errorf("%s: Where() = %q, want %q", tt.repo, got, tt.where)
		}
	}
}

func TestAge(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{20 * time.Second, "just now"},
		{time.Minute, "1 minute ago"},
		{12 * time.Minute, "12 minutes ago"},
		{3 * time.Hour, "3 hours ago"},
		{50 * time.Hour, "2 days ago"},
		{800 * 24 * time.Hour, "2 years ago"},
	}
	for _, tt := range tests {
		if got := Age(tt.in); got != tt.want {
			t.Errorf("Age(%s) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
