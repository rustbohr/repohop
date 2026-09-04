package git

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrMissing means the path does not exist on disk.
	ErrMissing = errors.New("path does not exist")
	// ErrNotRepo means the path exists but is not a git working tree.
	ErrNotRepo = errors.New("not a git repository")
)

// Status is a snapshot of one working tree.
type Status struct {
	Branch     string    // empty when detached or on an unborn branch
	Detached   bool      // HEAD points at a commit, not a branch
	Unborn     bool      // a fresh repo with no commits yet
	Head       string    // short HEAD sha; empty when unborn
	Dirty      bool      // tracked files modified (untracked files do not count)
	Upstream   string    // e.g. "origin/master"; empty when untracked
	Ahead      int       // commits ahead of upstream
	Behind     int       // commits behind upstream
	LastCommit time.Time // committer date of HEAD; zero when unborn
}

// Ref is the human-facing name of what HEAD is on.
func (s Status) Ref() string {
	switch {
	case s.Unborn:
		return "(unborn)"
	case s.Detached:
		return "(detached @ " + s.Head + ")"
	default:
		return s.Branch
	}
}

// Check reports whether dir is a usable git working tree.
func (r *Runner) Check(ctx context.Context, dir string) error {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ErrMissing
	}
	if _, err := r.run(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		return ErrNotRepo
	}
	return nil
}

// Status collects the state of one working tree in two git invocations.
func (r *Runner) Status(ctx context.Context, dir string) (Status, error) {
	var st Status
	if err := r.Check(ctx, dir); err != nil {
		return st, err
	}

	out, err := r.run(ctx, dir, "status", "--porcelain=v2", "--branch", "--untracked-files=no")
	if err != nil {
		return st, err
	}
	for _, line := range lines(out) {
		if !strings.HasPrefix(line, "# ") {
			// Any entry line means a tracked file differs from HEAD.
			st.Dirty = true
			continue
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "# "), " ")
		if !ok {
			continue
		}
		switch key {
		case "branch.oid":
			if value == "(initial)" {
				st.Unborn = true
			} else if len(value) >= 7 {
				st.Head = value[:7]
			}
		case "branch.head":
			if value == "(detached)" {
				st.Detached = true
			} else {
				st.Branch = value
			}
		case "branch.upstream":
			st.Upstream = value
		case "branch.ab":
			st.Ahead, st.Behind = parseAheadBehind(value)
		}
	}

	if !st.Unborn {
		if out, err := r.run(ctx, dir, "log", "-1", "--format=%cI"); err == nil {
			if t, perr := time.Parse(time.RFC3339, strings.TrimSpace(out)); perr == nil {
				st.LastCommit = t
			}
		}
	}
	return st, nil
}

// parseAheadBehind parses git's "+3 -1" ahead/behind field.
func parseAheadBehind(value string) (ahead, behind int) {
	for _, field := range strings.Fields(value) {
		if len(field) < 2 {
			continue
		}
		n, err := strconv.Atoi(field[1:])
		if err != nil {
			continue
		}
		switch field[0] {
		case '+':
			ahead = n
		case '-':
			behind = n
		}
	}
	return ahead, behind
}
