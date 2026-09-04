package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The tests in this package drive the real git binary against throwaway
// repositories under t.TempDir(). Slower than mocking exec, and the only way
// to be sure repohop's semantics match git's own.

func testRunner() *Runner { return &Runner{} }

func ctx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

// git runs a git command in dir and fails the test if it errors.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(environ(),
		"GIT_AUTHOR_NAME=repohop test", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=repohop test", "GIT_COMMITTER_EMAIL=test@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// writeFile writes a file inside a repository.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commit stages everything and records a commit.
func commit(t *testing.T, dir, message string) {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "--no-gpg-sign", "-m", message)
}

// initRepo creates a standalone repository on branch master with one commit.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "-c", "init.defaultBranch=master", "init", "-q", ".")
	gitRun(t, dir, "config", "user.name", "repohop test")
	gitRun(t, dir, "config", "user.email", "test@example.invalid")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	// Windows runners default to core.autocrlf=true, which would rewrite the
	// line endings of files these tests wrote byte for byte.
	gitRun(t, dir, "config", "core.autocrlf", "false")
	writeFile(t, dir, "README.md", "hello\n")
	commit(t, dir, "initial commit")
	return dir
}

// initOrigin creates a bare repository seeded with one commit on master, to
// stand in for a remote.
func initOrigin(t *testing.T) string {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, filepath.Dir(origin), "-c", "init.defaultBranch=master", "init", "--bare", "-q", origin)

	seed := initRepo(t)
	gitRun(t, seed, "remote", "add", "origin", origin)
	gitRun(t, seed, "push", "-q", "-u", "origin", "master")
	return origin
}

// clone clones a repository into a fresh temporary directory.
func clone(t *testing.T, origin string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "work")
	gitRun(t, filepath.Dir(dir), "clone", "-q", origin, dir)
	gitRun(t, dir, "config", "user.name", "repohop test")
	gitRun(t, dir, "config", "user.email", "test@example.invalid")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	// Windows runners default to core.autocrlf=true, which would rewrite the
	// line endings of files these tests wrote byte for byte.
	gitRun(t, dir, "config", "core.autocrlf", "false")
	return dir
}
