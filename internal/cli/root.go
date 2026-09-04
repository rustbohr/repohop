// Package cli defines the command-line surface of repohop.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"

	"github.com/rustbohr/repohop/internal/logging"
	"github.com/spf13/cobra"
)

// Exit codes, per the plan: 0 all good, 1 partial failure, 2 usage/config error.
const (
	exitOK      = 0
	exitPartial = 1
	exitUsage   = 2
)

// usageError marks an error as a usage or configuration problem (exit code 2).
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

func usagef(format string, a ...any) error {
	return usageError{fmt.Errorf(format, a...)}
}

// partialError marks an error as "some repo failed" (exit code 1).
type partialError struct{ err error }

func (e partialError) Error() string { return e.err.Error() }
func (e partialError) Unwrap() error { return e.err }

// Global flags shared by every subcommand.
var (
	flagConfig  string
	flagProject string
)

func newRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "repohop",
		Short: "Drive a set of git repositories as one unit",
		Long: "repohop shows every repository in a project at a glance and switches\n" +
			"them all onto the same branch.\n\n" +
			"Run without arguments in a terminal to start the interactive UI.",
		Args:          cobra.NoArgs,
		RunE:          runDefault,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}

	root.PersistentFlags().StringVar(&flagConfig, "config", "", "path to a config file (overrides discovery)")
	root.PersistentFlags().StringVar(&flagProject, "project", "", "project to act on (defaults to the active project)")

	root.SetVersionTemplate("repohop {{.Version}}\n")
	root.AddCommand(
		newVersionCmd(version),
		newProjectsCmd(),
		newConfigCmd(),
		newStatusCmd(),
		newSwitchCmd(),
		newFetchCmd(),
		newPullCmd(),
	)

	return root
}

// Execute runs the root command and exits the process with the right code.
func Execute(version string) {
	logging.Init() //nolint:errcheck // logging must never be why repohop stops
	defer logging.Close()
	defer recoverPanic()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	root := newRootCmd(version)
	if err := root.ExecuteContext(ctx); err != nil {
		if worthLogging(err) {
			logging.Log().Error("running "+strings.Join(os.Args[1:], " "), err)
		}
		fmt.Fprintln(os.Stderr, "repohop:", err)
		os.Exit(exitCodeFor(err))
	}
	os.Exit(exitOK)
}

// worthLogging keeps the log for things that went wrong, not for things the
// user was simply told. Being asked to name a project, or told there are none
// yet, is ordinary use — writing a log file for it would leave every new user
// with one before they have even started.
func worthLogging(err error) bool {
	_, usage := err.(usageError)
	return !usage
}

// recoverPanic turns a crash outside the interface into a short message and a
// log entry rather than a wall of stack trace.
func recoverPanic() {
	r := recover()
	if r == nil {
		return
	}
	logging.Log().Panic("running "+strings.Join(os.Args[1:], " "), r, debug.Stack())

	fmt.Fprintf(os.Stderr, "repohop: something went wrong: %v\n", r)
	if path := logging.Log().Path(); path != "" {
		fmt.Fprintf(os.Stderr, "repohop: this is a bug; the details are in %s\n", path)
	}
	logging.Close() //nolint:errcheck // already on the way out
	os.Exit(exitUsage)
}

func exitCodeFor(err error) int {
	switch err.(type) {
	case usageError:
		return exitUsage
	case partialError:
		return exitPartial
	default:
		return exitUsage
	}
}
