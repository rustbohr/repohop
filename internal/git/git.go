// Package git is the only place in repohop that executes the git binary.
//
// Every call goes through Runner.run: `git -C <dir> …` with an explicit
// timeout, captured stdout and stderr, and a typed error. Arguments are passed
// as a slice and never through a shell, so branch names with odd characters
// cannot escape.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout bounds a single git invocation that does not touch the
// network. Network operations take their own, longer timeout.
const (
	DefaultTimeout = 30 * time.Second
	NetworkTimeout = 5 * time.Minute
)

// Runner executes git commands. The zero value is usable and is what the
// package-level helpers use.
type Runner struct {
	// Binary is the git executable; empty means "git" from PATH.
	Binary string
	// Timeout bounds each local invocation; zero means DefaultTimeout.
	Timeout time.Duration
	// NetworkTimeout bounds fetch and pull; zero means NetworkTimeout.
	NetworkTimeout time.Duration
}

// Default is the Runner used by the package-level helpers.
var Default = &Runner{}

// Error is a failed git invocation, carrying enough detail for the UI to show
// the user the exact command they can rerun themselves.
type Error struct {
	Dir      string
	Args     []string
	ExitCode int
	Stderr   string
	Err      error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("%s: %s", e.Command(), msg)
}

func (e *Error) Unwrap() error { return e.Err }

// Command renders the invocation as the user would type it.
func (e *Error) Command() string {
	parts := append([]string{"git", "-C", e.Dir}, e.Args...)
	return strings.Join(parts, " ")
}

// ErrNotFound reports that the git binary is missing from PATH.
var ErrNotFound = errors.New("git executable not found in PATH")

func (r *Runner) binary() string {
	if r.Binary != "" {
		return r.Binary
	}
	return "git"
}

func (r *Runner) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultTimeout
}

func (r *Runner) networkTimeout() time.Duration {
	if r.NetworkTimeout > 0 {
		return r.NetworkTimeout
	}
	return NetworkTimeout
}

// run executes git in dir and returns trimmed stdout.
func (r *Runner) run(ctx context.Context, dir string, args ...string) (string, error) {
	return r.runTimeout(ctx, r.timeout(), dir, args...)
}

// runNet is run with the longer network timeout.
func (r *Runner) runNet(ctx context.Context, dir string, args ...string) (string, error) {
	return r.runTimeout(ctx, r.networkTimeout(), dir, args...)
}

func (r *Runner) runTimeout(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, r.binary(), full...)
	cmd.Env = environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return strings.TrimRight(stdout.String(), "\n"), nil
	}

	gerr := &Error{Dir: dir, Args: args, ExitCode: -1, Stderr: stderr.String(), Err: err}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		gerr.ExitCode = exitErr.ExitCode()
	}
	if errors.Is(err, exec.ErrNotFound) {
		gerr.Err = ErrNotFound
	}
	if ctx.Err() != nil {
		gerr.Err = fmt.Errorf("timed out after %s: %w", timeout, ctx.Err())
	}
	return strings.TrimRight(stdout.String(), "\n"), gerr
}

// environ returns the environment for a git child process: the inherited one
// plus settings that keep output parseable and stop git blocking on a prompt.
func environ() []string {
	env := os.Environ()
	return append(env,
		"GIT_TERMINAL_PROMPT=0", // never block asking for credentials
		"GIT_OPTIONAL_LOCKS=0",  // status must not take the index lock
		"LC_ALL=C",              // stable, parseable messages
		"GIT_PAGER=cat",
		"PAGER=cat",
	)
}

// lines splits git output into non-empty lines.
func lines(out string) []string {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// Version returns the git version string, e.g. "2.34.1".
func (r *Runner) Version(ctx context.Context) (string, error) {
	out, err := r.run(ctx, ".", "--version")
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(out), "git version "), nil
}

// MinMajor and MinMinor are the oldest git repohop is known to work with.
// `git status --porcelain=v2` arrived in 2.11; everything else repohop runs is
// older than that.
const (
	MinMajor = 2
	MinMinor = 11
)

// ErrTooOld reports a git that predates what repohop relies on.
type ErrTooOld struct{ Version string }

func (e *ErrTooOld) Error() string {
	return fmt.Sprintf("git %s is too old: repohop needs %d.%d or newer", e.Version, MinMajor, MinMinor)
}

// Require checks that a usable git is on PATH. It is called once at startup so
// a missing or ancient git is one clear message rather than a failure per
// repository.
func (r *Runner) Require(ctx context.Context) error {
	version, err := r.Version(ctx)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	major, minor := parseVersion(version)
	if major < MinMajor || (major == MinMajor && minor < MinMinor) {
		return &ErrTooOld{Version: version}
	}
	return nil
}

// parseVersion pulls the major and minor numbers out of a git version string.
// An unparseable version is treated as new enough: better to try than to
// refuse to run against, say, a vendor build with an odd version string.
func parseVersion(version string) (major, minor int) {
	fields := strings.SplitN(version, ".", 3)
	if len(fields) < 2 {
		return MinMajor, MinMinor
	}
	major, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil {
		return MinMajor, MinMinor
	}
	minor, err = strconv.Atoi(strings.TrimSpace(fields[1]))
	if err != nil {
		return MinMajor, MinMinor
	}
	return major, minor
}
