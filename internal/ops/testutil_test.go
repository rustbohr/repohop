package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rustbohr/repohop/internal/model"
)

// These tests drive real git repositories under t.TempDir(): the switch rules
// are only worth anything if they match what git actually does.

func ctx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", "LC_ALL=C",
		"GIT_AUTHOR_NAME=repohop test", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=repohop test", "GIT_COMMITTER_EMAIL=test@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, message string) {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "--no-gpg-sign", "-m", message)
}

func configure(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "config", "user.name", "repohop test")
	gitRun(t, dir, "config", "user.email", "test@example.invalid")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
}

// initOrigin creates a bare repository seeded with master and any extra
// branches, standing in for a remote.
func initOrigin(t *testing.T, branches ...string) string {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, filepath.Dir(origin), "-c", "init.defaultBranch=master", "init", "--bare", "-q", origin)

	seed := filepath.Join(t.TempDir(), "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seed, "-c", "init.defaultBranch=master", "init", "-q", ".")
	configure(t, seed)
	writeFile(t, seed, "README.md", "hello\n")
	commit(t, seed, "initial commit")
	gitRun(t, seed, "remote", "add", "origin", origin)
	gitRun(t, seed, "push", "-q", "-u", "origin", "master")

	for _, branch := range branches {
		gitRun(t, seed, "checkout", "-q", "-b", branch)
		writeFile(t, seed, "f.txt", branch+"\n")
		commit(t, seed, "work on "+branch)
		gitRun(t, seed, "push", "-q", "origin", branch)
		gitRun(t, seed, "checkout", "-q", "master")
	}
	return origin
}

func clone(t *testing.T, origin, name string) model.Repo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	gitRun(t, filepath.Dir(dir), "clone", "-q", origin, dir)
	configure(t, dir)
	return model.Repo{Name: name, Path: dir}
}

// branchOf reports the branch a repository is currently on.
func branchOf(t *testing.T, repo model.Repo) string {
	t.Helper()
	return gitRun(t, repo.Path, "rev-parse", "--abbrev-ref", "HEAD")
}

func testRunner() *Runner { return New(4) }
