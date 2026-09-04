// Package ops implements repohop's operations on top of internal/git and
// internal/task. Both the CLI and the TUI drive these, so the semantics —
// especially the switch rules — exist in exactly one place.
package ops

import (
	"context"
	"errors"

	"github.com/rustbohr/repohop/internal/git"
	"github.com/rustbohr/repohop/internal/model"
	"github.com/rustbohr/repohop/internal/task"
)

// Runner performs operations across a set of repositories.
type Runner struct {
	Git         *git.Runner
	Concurrency int
	// Remote is the remote repohop reasons about; empty means origin.
	Remote string
}

// New returns a Runner using the default git binary.
func New(concurrency int) *Runner {
	return &Runner{Git: git.Default, Concurrency: concurrency}
}

func (r *Runner) gitRunner() *git.Runner {
	if r.Git != nil {
		return r.Git
	}
	return git.Default
}

func (r *Runner) remote() string {
	if r.Remote != "" {
		return r.Remote
	}
	return git.DefaultRemote
}

// StatusStream reports each repository's state as it lands.
func (r *Runner) StatusStream(ctx context.Context, repos []model.Repo) <-chan task.Result[git.Status] {
	return task.Stream(ctx, repos, r.Concurrency, func(ctx context.Context, repo model.Repo) (git.Status, error) {
		return r.gitRunner().Status(ctx, repo.Path)
	})
}

// Status collects every repository's state, in project order. A repository
// that could not be read carries its error and is never dropped.
func (r *Runner) Status(ctx context.Context, repos []model.Repo) []model.RepoState {
	results := task.Collect(ctx, repos, r.Concurrency, func(ctx context.Context, repo model.Repo) (git.Status, error) {
		return r.gitRunner().Status(ctx, repo.Path)
	})
	states := make([]model.RepoState, len(results))
	for i, result := range results {
		states[i] = model.RepoState{Repo: result.Repo, Status: result.Value, Err: result.Err}
	}
	return states
}

// Branches enumerates every repository's branches, keyed by repository path.
// Repositories that could not be read are reported separately rather than
// failing the whole enumeration.
func (r *Runner) Branches(ctx context.Context, repos []model.Repo) (map[string]git.BranchSet, []OpResult) {
	results := task.Collect(ctx, repos, r.Concurrency, func(ctx context.Context, repo model.Repo) (git.BranchSet, error) {
		if err := r.gitRunner().Check(ctx, repo.Path); err != nil {
			return git.BranchSet{}, err
		}
		return r.gitRunner().Branches(ctx, repo.Path, r.remote())
	})

	sets := make(map[string]git.BranchSet, len(results))
	var failures []OpResult
	for _, result := range results {
		if result.Err != nil {
			failures = append(failures, OpResult{Repo: result.Repo, Err: result.Err})
			continue
		}
		sets[result.Repo.Path] = result.Value
	}
	return sets, failures
}

// OpResult is the outcome of a simple per-repository operation.
type OpResult struct {
	Repo model.Repo
	// Note explains a non-failure outcome, e.g. "no remote configured".
	Note string
	Err  error
}

// OK reports whether the operation succeeded.
func (o OpResult) OK() bool { return o.Err == nil }

// Fetch updates remote-tracking refs everywhere. Read-only, so it fans out
// across the worker pool; report, when non-nil, is called as each repository
// lands, in completion order.
func (r *Runner) Fetch(ctx context.Context, repos []model.Repo, report func(OpResult)) []OpResult {
	results := make([]task.Result[string], len(repos))
	for i := range results {
		results[i] = task.Result[string]{Index: i, Repo: repos[i]}
	}
	stream := task.Stream(ctx, repos, r.Concurrency, func(ctx context.Context, repo model.Repo) (string, error) {
		return r.fetchOne(ctx, repo)
	})
	for result := range stream {
		results[result.Index] = result
		if report != nil {
			report(toOpResult(result))
		}
	}
	return toOpResults(results)
}

func (r *Runner) fetchOne(ctx context.Context, repo model.Repo) (string, error) {
	if err := r.gitRunner().Check(ctx, repo.Path); err != nil {
		return "", err
	}
	err := r.gitRunner().Fetch(ctx, repo.Path, r.remote())
	if errors.Is(err, git.ErrNoRemote) {
		return "no remote configured", nil
	}
	return "", err
}

// Pull fast-forwards every repository's current branch. A write operation, so
// it runs sequentially; report, when non-nil, is called after each repository.
func (r *Runner) Pull(ctx context.Context, repos []model.Repo, report func(OpResult)) []OpResult {
	var wrapped func(task.Result[string])
	if report != nil {
		wrapped = func(result task.Result[string]) { report(toOpResult(result)) }
	}
	results := task.Sequential(ctx, repos, func(ctx context.Context, repo model.Repo) (string, error) {
		return r.pullOne(ctx, repo)
	}, wrapped)
	return toOpResults(results)
}

func (r *Runner) pullOne(ctx context.Context, repo model.Repo) (string, error) {
	status, err := r.gitRunner().Status(ctx, repo.Path)
	if err != nil {
		return "", err
	}
	switch {
	case status.Detached:
		return "detached HEAD, not pulled", nil
	case status.Unborn:
		return "no commits yet, not pulled", nil
	case status.Upstream == "":
		return "no upstream, not pulled", nil
	case status.Dirty:
		return "dirty, not pulled", nil
	}

	// git pull does its own fetch, so the remote-tracking refs need not be
	// fresh; whether anything moved is decided by comparing HEAD afterwards.
	before := status.Head
	if err := r.gitRunner().Pull(ctx, repo.Path); err != nil {
		if errors.Is(err, git.ErrNotFastForward) {
			return "diverged from " + status.Upstream + ", not pulled", nil
		}
		return "", err
	}
	after, err := r.gitRunner().Status(ctx, repo.Path)
	if err == nil && after.Head == before {
		return "already up to date", nil
	}
	return "", nil
}

func toOpResult(result task.Result[string]) OpResult {
	return OpResult{Repo: result.Repo, Note: result.Value, Err: result.Err}
}

func toOpResults(results []task.Result[string]) []OpResult {
	out := make([]OpResult, len(results))
	for i, result := range results {
		out[i] = toOpResult(result)
	}
	return out
}

// Failures counts the results that errored.
func Failures(results []OpResult) int {
	n := 0
	for _, result := range results {
		if result.Err != nil {
			n++
		}
	}
	return n
}
