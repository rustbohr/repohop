// Package cli defines the command-line surface of repohop.
package cli

import (
	"fmt"
	"os"

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
	)

	return root
}

// Execute runs the root command and exits the process with the right code.
func Execute(version string) {
	root := newRootCmd(version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "repohop:", err)
		os.Exit(exitCodeFor(err))
	}
	os.Exit(exitOK)
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
