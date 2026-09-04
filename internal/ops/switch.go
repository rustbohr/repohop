package ops

import (
	"context"
	"errors"

	"github.com/rustbohr/repohop/internal/git"
	"github.com/rustbohr/repohop/internal/model"
	"github.com/rustbohr/repohop/internal/task"
)

// DirtyPolicy decides what happens to a repository with local modifications.
type DirtyPolicy int

const (
	// DirtySkip leaves the repository alone and reports it. The default.
	DirtySkip DirtyPolicy = iota
	// DirtyStash stashes the working tree (including untracked files) and
	// records the stash so it can be offered for restore afterwards.
	DirtyStash
)

// SwitchOptions configure a switch across repositories.
type SwitchOptions struct {
	Branch string
	// Pull fast-forwards each repository after a successful checkout.
	Pull bool
	// Dirty decides what to do with repositories that have local changes.
	Dirty DirtyPolicy
}

// Outcome is what happened to one repository during a switch.
type Outcome int

const (
	// OutcomeSwitched means the repository moved to the requested branch.
	OutcomeSwitched Outcome = iota
	// OutcomeUnchanged means it was already on the requested branch.
	OutcomeUnchanged
	// OutcomeNoBranch means the branch exists neither locally nor on the
	// remote, so the repository was left untouched. repohop never creates a
	// branch implicitly.
	OutcomeNoBranch
	// OutcomeSkippedDirty means the working tree had local changes.
	OutcomeSkippedDirty
	// OutcomeFailed means a git command failed.
	OutcomeFailed
)

// String renders the outcome for the summary table.
func (o Outcome) String() string {
	switch o {
	case OutcomeSwitched:
		return "switched"
	case OutcomeUnchanged:
		return "unchanged"
	case OutcomeNoBranch:
		return "no such branch"
	case OutcomeSkippedDirty:
		return "dirty, skipped"
	default:
		return "failed"
	}
}

// SwitchResult is one row of the summary table.
type SwitchResult struct {
	Repo      model.Repo
	OldBranch string
	NewBranch string
	Outcome   Outcome
	// Note carries the detail the summary shows in parentheses.
	Note string
	// StashRef is the id of the stash taken for this repository, if any.
	StashRef string
	Err      error
}

// OK reports whether the repository ended up where it was asked to.
func (s SwitchResult) OK() bool {
	return s.Outcome == OutcomeSwitched || s.Outcome == OutcomeUnchanged
}

// Preflight is what a switch would run into: the state of every repository and
// which of them carry the branch.
type Preflight struct {
	Branch string
	States []model.RepoState
	// Presence mirrors States, index for index.
	Presence []model.BranchPresence
}

// Dirty lists the repositories with local modifications.
func (p Preflight) Dirty() []model.RepoState {
	var dirty []model.RepoState
	for _, state := range p.States {
		if state.OK() && state.Status.Dirty {
			dirty = append(dirty, state)
		}
	}
	return dirty
}

// Carriers counts the repositories that have the branch.
func (p Preflight) Carriers() int {
	n := 0
	for _, presence := range p.Presence {
		if presence.Any() {
			n++
		}
	}
	return n
}

// Preflight inspects every repository before a switch runs, so the caller can
// offer a choice about dirty trees instead of aborting.
func (r *Runner) Preflight(ctx context.Context, repos []model.Repo, branch string) Preflight {
	type check struct {
		status   git.Status
		presence model.BranchPresence
	}
	results := task.Collect(ctx, repos, r.Concurrency, func(ctx context.Context, repo model.Repo) (check, error) {
		status, err := r.gitRunner().Status(ctx, repo.Path)
		if err != nil {
			return check{}, err
		}
		set, err := r.gitRunner().Branches(ctx, repo.Path, r.remote())
		if err != nil {
			return check{status: status}, err
		}
		local, remote := set.Has(branch)
		return check{status: status, presence: model.BranchPresence{Repo: repo, Local: local, Remote: remote}}, nil
	})

	pre := Preflight{Branch: branch}
	for _, result := range results {
		pre.States = append(pre.States, model.RepoState{Repo: result.Repo, Status: result.Value.status, Err: result.Err})
		pre.Presence = append(pre.Presence, model.BranchPresence{
			Repo:   result.Repo,
			Local:  result.Value.presence.Local,
			Remote: result.Value.presence.Remote,
		})
	}
	return pre
}

// Switch puts every repository onto opts.Branch. Write operations run
// sequentially: concurrent checkout output is hard to reason about when
// something fails halfway. report, when non-nil, is called after each
// repository.
func (r *Runner) Switch(ctx context.Context, repos []model.Repo, opts SwitchOptions, report func(SwitchResult)) []SwitchResult {
	var wrapped func(task.Result[SwitchResult])
	if report != nil {
		wrapped = func(result task.Result[SwitchResult]) { report(result.Value) }
	}
	results := task.Sequential(ctx, repos, func(ctx context.Context, repo model.Repo) (SwitchResult, error) {
		return r.switchOne(ctx, repo, opts), nil
	}, wrapped)

	out := make([]SwitchResult, len(results))
	for i, result := range results {
		out[i] = result.Value
	}
	return out
}

// switchOne carries the switch semantics, deliberately conservative: never
// create a branch, never force, never touch a dirty tree without being told.
func (r *Runner) switchOne(ctx context.Context, repo model.Repo, opts SwitchOptions) SwitchResult {
	g := r.gitRunner()
	result := SwitchResult{Repo: repo, NewBranch: opts.Branch}

	status, err := g.Status(ctx, repo.Path)
	if err != nil {
		return fail(result, err)
	}
	result.OldBranch = status.Ref()
	result.NewBranch = status.Ref() // corrected once we know we can move

	if status.Unborn {
		result.Outcome = OutcomeNoBranch
		result.Note = "no commits yet"
		return result
	}

	set, err := g.Branches(ctx, repo.Path, r.remote())
	if err != nil {
		return fail(result, err)
	}
	local, remote := set.Has(opts.Branch)
	if !local && !remote {
		result.Outcome = OutcomeNoBranch
		return result
	}

	alreadyThere := !status.Detached && status.Branch == opts.Branch
	if status.Dirty {
		switch opts.Dirty {
		case DirtyStash:
			ref, err := g.Stash(ctx, repo.Path, "repohop: switch to "+opts.Branch)
			if err != nil && !errors.Is(err, git.ErrNothingToStash) {
				return fail(result, err)
			}
			result.StashRef = ref
		default:
			result.Outcome = OutcomeSkippedDirty
			return result
		}
	}

	if alreadyThere {
		result.Outcome = OutcomeUnchanged
	} else {
		if err := g.Checkout(ctx, repo.Path, opts.Branch); err != nil {
			return fail(result, err)
		}
		result.Outcome = OutcomeSwitched
	}
	result.NewBranch = opts.Branch

	r.syncAfterCheckout(ctx, repo, opts, remote, &result)
	if result.StashRef != "" {
		result.Note = appendNote(result.Note, "stashed")
	}
	return result
}

// syncAfterCheckout sets the upstream when the remote carries the branch and
// then fast-forwards. Anything that is not a clean fast-forward is reported,
// never forced.
func (r *Runner) syncAfterCheckout(ctx context.Context, repo model.Repo, opts SwitchOptions, remote bool, result *SwitchResult) {
	g := r.gitRunner()

	if !remote {
		result.Note = appendNote(result.Note, "local only, not pulled")
		return
	}

	status, err := g.Status(ctx, repo.Path)
	if err != nil {
		result.Note = appendNote(result.Note, err.Error())
		return
	}
	if status.Upstream == "" {
		if err := g.SetUpstream(ctx, repo.Path, opts.Branch, r.remote()); err != nil {
			result.Note = appendNote(result.Note, "could not set upstream")
			return
		}
		if status, err = g.Status(ctx, repo.Path); err != nil {
			result.Note = appendNote(result.Note, err.Error())
			return
		}
	}

	if !opts.Pull {
		return
	}
	if status.Dirty {
		result.Note = appendNote(result.Note, "dirty, not pulled")
		return
	}
	switch err := g.Pull(ctx, repo.Path); {
	case err == nil:
	case errors.Is(err, git.ErrNotFastForward):
		result.Note = appendNote(result.Note, "not a fast-forward, not pulled")
	default:
		result.Note = appendNote(result.Note, "pull failed: "+err.Error())
	}
}

func fail(result SwitchResult, err error) SwitchResult {
	result.Outcome = OutcomeFailed
	result.Err = err
	if result.NewBranch == "" {
		result.NewBranch = result.OldBranch
	}
	return result
}

func appendNote(existing, note string) string {
	if existing == "" {
		return note
	}
	return existing + ", " + note
}

// SwitchFailures counts the repositories that did not end up on the branch.
func SwitchFailures(results []SwitchResult) int {
	n := 0
	for _, result := range results {
		if !result.OK() {
			n++
		}
	}
	return n
}
