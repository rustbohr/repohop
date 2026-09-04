// Package model holds repohop's domain types: the projects and repositories a
// user configures, and the state repohop observes about them.
package model

import (
	"sort"
	"strconv"
	"time"

	"github.com/rustbohr/repohop/internal/git"
)

// Repo is one repository in a project, with its path already expanded and made
// absolute.
type Repo struct {
	// Name is the display name: the directory's base name unless the config
	// overrides it.
	Name string
	// Path is the absolute path to the working tree.
	Path string
}

// Project is a named set of repositories driven as one unit.
type Project struct {
	Name string
	// Base is the directory bare repo entries were resolved against, kept for
	// display and for writing the project back out.
	Base  string
	Repos []Repo
	// Source is the config file the project came from.
	Source string
}

// Repo returns the repository with the given name.
func (p Project) Repo(name string) (Repo, bool) {
	for _, r := range p.Repos {
		if r.Name == name {
			return r, true
		}
	}
	return Repo{}, false
}

// RepoState is what repohop observed about one repository. A repository that
// could not be read carries Err and is still shown, never silently dropped.
type RepoState struct {
	Repo   Repo
	Status git.Status
	Err    error
}

// OK reports whether the repository was read successfully.
func (s RepoState) OK() bool { return s.Err == nil }

// BranchPresence records where one branch lives in one repository.
type BranchPresence struct {
	Repo   Repo
	Local  bool
	Remote bool
}

// Any reports whether the repository carries the branch at all.
func (p BranchPresence) Any() bool { return p.Local || p.Remote }

// Where renders the presence as it appears in the picker preview.
func (p BranchPresence) Where() string {
	switch {
	case p.Local && p.Remote:
		return "local " + git.DefaultRemote
	case p.Local:
		return "local"
	case p.Remote:
		return git.DefaultRemote
	default:
		return "—"
	}
}

// BranchInfo is one candidate in the branch picker: a branch name and which of
// the selected repositories carry it.
type BranchInfo struct {
	Name string
	// In holds one entry per selected repository, in project order.
	In []BranchPresence
}

// Count is the number of repositories carrying the branch.
func (b BranchInfo) Count() int {
	n := 0
	for _, p := range b.In {
		if p.Any() {
			n++
		}
	}
	return n
}

// CollectBranches builds the picker's candidate list: the union of local and
// remote branches across the given repositories, sorted by how many
// repositories carry each branch and then alphabetically.
func CollectBranches(repos []Repo, sets map[string]git.BranchSet) []BranchInfo {
	names := map[string]struct{}{}
	for _, set := range sets {
		for _, name := range set.Names() {
			names[name] = struct{}{}
		}
	}

	infos := make([]BranchInfo, 0, len(names))
	for name := range names {
		info := BranchInfo{Name: name, In: make([]BranchPresence, 0, len(repos))}
		for _, repo := range repos {
			local, remote := sets[repo.Path].Has(name)
			info.In = append(info.In, BranchPresence{Repo: repo, Local: local, Remote: remote})
		}
		infos = append(infos, info)
	}

	sort.Slice(infos, func(i, j int) bool {
		if ci, cj := infos[i].Count(), infos[j].Count(); ci != cj {
			return ci > cj
		}
		return infos[i].Name < infos[j].Name
	})
	return infos
}

// Age renders a duration the way the dashboard's LAST COMMIT column does.
func Age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/24), "day")
	default:
		return plural(int(d.Hours()/24/365), "year")
	}
}

func plural(n int, unit string) string {
	s := strconv.Itoa(n) + " " + unit
	if n != 1 {
		s += "s"
	}
	return s + " ago"
}
