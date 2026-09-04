// Package scan discovers git repositories on disk for the setup flow.
package scan

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultDepth is how far below the scanned directory repohop looks. Deep
// enough for the usual ~/src/<org>/<repo> layout, shallow enough to stay fast.
const DefaultDepth = 3

// Repo is one discovered repository.
type Repo struct {
	// Path is absolute.
	Path string
	// Name is the directory's base name.
	Name string
	// Rel is the path relative to the scanned root, which becomes the config
	// entry when the root is used as the project base.
	Rel string
}

// Options control a scan.
type Options struct {
	// Root is the directory to scan; it is expanded and made absolute.
	Root string
	// Depth is how many directory levels below the root to descend.
	Depth int
	// IncludeHidden descends into dot-directories too.
	IncludeHidden bool
}

// Find walks root looking for git working trees. A directory that is itself a
// repository is not descended into: repohop coordinates independent repos, and
// nested checkouts are almost always vendored copies.
func Find(ctx context.Context, opts Options) ([]Repo, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	depth := opts.Depth
	if depth <= 0 {
		depth = DefaultDepth
	}

	var found []Repo
	var walk func(dir string, level int) error
	walk = func(dir string, level int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if isRepo(dir) {
			rel, err := filepath.Rel(root, dir)
			if err != nil {
				rel = dir
			}
			found = append(found, Repo{Path: dir, Name: filepath.Base(dir), Rel: rel})
			return nil
		}
		if level >= depth {
			return nil
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			// An unreadable directory is skipped, not fatal: a scan of a home
			// directory routinely meets a few.
			return nil
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !opts.IncludeHidden && strings.HasPrefix(name, ".") {
				continue
			}
			if err := walk(filepath.Join(dir, name), level+1); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walk(root, 0); err != nil {
		return nil, err
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Rel < found[j].Rel })
	return found, nil
}

// isRepo reports whether dir holds a .git directory or file (a worktree).
func isRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
