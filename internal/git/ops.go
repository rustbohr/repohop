package git

import (
	"context"
	"errors"
	"strings"
)

var (
	// ErrNotFastForward means a pull would need a merge or rebase.
	ErrNotFastForward = errors.New("not a fast-forward")
	// ErrNothingToStash means the working tree was already clean.
	ErrNothingToStash = errors.New("nothing to stash")
	// ErrNoRemote means the repository has no remotes configured.
	ErrNoRemote = errors.New("no remote configured")
	// ErrStashGone means a recorded stash entry is no longer in the stash list.
	ErrStashGone = errors.New("stash entry no longer exists")
)

// Remotes lists the configured remote names.
func (r *Runner) Remotes(ctx context.Context, dir string) ([]string, error) {
	out, err := r.run(ctx, dir, "remote")
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// HasRemote reports whether the named remote is configured.
func (r *Runner) HasRemote(ctx context.Context, dir, remote string) bool {
	if remote == "" {
		remote = DefaultRemote
	}
	remotes, err := r.Remotes(ctx, dir)
	if err != nil {
		return false
	}
	for _, name := range remotes {
		if name == remote {
			return true
		}
	}
	return false
}

// Fetch updates remote-tracking refs and prunes deleted ones. A repository
// with no remote is reported with ErrNoRemote rather than failing obscurely.
func (r *Runner) Fetch(ctx context.Context, dir, remote string) error {
	if remote == "" {
		remote = DefaultRemote
	}
	if !r.HasRemote(ctx, dir, remote) {
		return ErrNoRemote
	}
	_, err := r.runNet(ctx, dir, "fetch", "--prune", "--quiet", remote)
	return err
}

// Checkout switches the working tree to an existing branch. It never creates a
// branch from scratch: git's own DWIM rule may create a local branch tracking
// <remote>/<branch>, which is exactly the origin-only case we want.
func (r *Runner) Checkout(ctx context.Context, dir, branch string) error {
	_, err := r.run(ctx, dir, "checkout", branch, "--")
	return err
}

// SetUpstream points the local branch at <remote>/<branch>.
func (r *Runner) SetUpstream(ctx context.Context, dir, branch, remote string) error {
	if remote == "" {
		remote = DefaultRemote
	}
	_, err := r.run(ctx, dir, "branch", "--set-upstream-to="+remote+"/"+branch, branch)
	return err
}

// Pull fast-forwards the current branch, never merging or rebasing. A
// divergent branch is reported as ErrNotFastForward and left untouched.
func (r *Runner) Pull(ctx context.Context, dir string) error {
	_, err := r.runNet(ctx, dir, "pull", "--ff-only")
	if err == nil {
		return nil
	}
	var gerr *Error
	if errors.As(err, &gerr) && isNotFastForward(gerr.Stderr) {
		return ErrNotFastForward
	}
	return err
}

func isNotFastForward(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "not possible to fast-forward") ||
		strings.Contains(s, "need to specify how to reconcile divergent branches") ||
		strings.Contains(s, "diverged")
}

// Stash saves the working tree, including untracked files, and returns the
// commit id of the new stash entry.
func (r *Runner) Stash(ctx context.Context, dir, message string) (string, error) {
	args := []string{"stash", "push", "--include-untracked"}
	if message != "" {
		args = append(args, "--message", message)
	}
	out, err := r.run(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	if strings.Contains(out, "No local changes to save") {
		return "", ErrNothingToStash
	}
	sha, err := r.run(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/stash")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

// StashPop restores the stash entry with the given commit id. Entries are
// addressed by id rather than by stash@{0} because other stashes may have been
// pushed in between.
func (r *Runner) StashPop(ctx context.Context, dir, sha string) error {
	ref, err := r.stashRef(ctx, dir, sha)
	if err != nil {
		return err
	}
	_, err = r.run(ctx, dir, "stash", "pop", ref)
	return err
}

// stashRef finds the stash@{n} slot currently holding the given commit id.
func (r *Runner) stashRef(ctx context.Context, dir, sha string) (string, error) {
	out, err := r.run(ctx, dir, "stash", "list", "--format=%H %gd")
	if err != nil {
		return "", err
	}
	for _, line := range lines(out) {
		id, ref, ok := strings.Cut(line, " ")
		if ok && id == sha {
			return ref, nil
		}
	}
	return "", ErrStashGone
}
