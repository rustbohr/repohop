package git

import (
	"context"
	"sort"
	"strings"
)

// DefaultRemote is the remote repohop reasons about. Branches that exist only
// on some other remote are deliberately ignored.
const DefaultRemote = "origin"

// BranchSet is the set of branch names one repository carries, split by where
// each name lives. Remote names are stored with the remote prefix stripped, so
// "origin/feat/x" appears as "feat/x" and lines up with the local name.
type BranchSet struct {
	Local  map[string]struct{}
	Remote map[string]struct{}
}

// Has reports where a branch name exists in this repository.
func (b BranchSet) Has(name string) (local, remote bool) {
	_, local = b.Local[name]
	_, remote = b.Remote[name]
	return local, remote
}

// Any reports whether the branch exists at all, locally or on the remote.
func (b BranchSet) Any(name string) bool {
	local, remote := b.Has(name)
	return local || remote
}

// Names returns the sorted union of local and remote branch names.
func (b BranchSet) Names() []string {
	seen := make(map[string]struct{}, len(b.Local)+len(b.Remote))
	names := make([]string, 0, len(b.Local)+len(b.Remote))
	for _, set := range []map[string]struct{}{b.Local, b.Remote} {
		for name := range set {
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Branches enumerates the local branches and the branches on remote in a
// single git invocation. A remote of "" means DefaultRemote.
func (r *Runner) Branches(ctx context.Context, dir, remote string) (BranchSet, error) {
	if remote == "" {
		remote = DefaultRemote
	}
	set := BranchSet{Local: map[string]struct{}{}, Remote: map[string]struct{}{}}

	out, err := r.run(ctx, dir, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes/"+remote)
	if err != nil {
		return set, err
	}

	localPrefix := "refs/heads/"
	remotePrefix := "refs/remotes/" + remote + "/"
	for _, ref := range lines(out) {
		switch {
		case strings.HasPrefix(ref, localPrefix):
			set.Local[strings.TrimPrefix(ref, localPrefix)] = struct{}{}
		case strings.HasPrefix(ref, remotePrefix):
			name := strings.TrimPrefix(ref, remotePrefix)
			if name == "HEAD" { // the symbolic origin/HEAD is not a branch
				continue
			}
			set.Remote[name] = struct{}{}
		}
	}
	return set, nil
}

// RefExists reports whether a fully-qualified ref resolves to a commit.
func (r *Runner) RefExists(ctx context.Context, dir, ref string) bool {
	_, err := r.run(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}
