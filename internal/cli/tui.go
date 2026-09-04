package cli

import (
	"errors"
	"os"

	"github.com/rustbohr/repohop/internal/config"
	"github.com/rustbohr/repohop/internal/model"
	"github.com/rustbohr/repohop/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// isTerminal reports whether stdout is a terminal. When it is not, repohop
// never starts Bubble Tea: it runs the equivalent non-interactive command, so
// the tool stays usable in scripts, pipes and CI.
func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// runDefault is what `repohop` with no subcommand does.
func runDefault(cmd *cobra.Command, args []string) error {
	if !isTerminal() {
		status := newStatusCmd()
		status.SetArgs(args)
		status.SetOut(cmd.OutOrStdout())
		status.SetErr(cmd.ErrOrStderr())
		return status.RunE(status, args)
	}

	if err := requireGit(cmd.Context()); err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// The TUI opens on the project list. Only an explicit --project skips it:
	// asking for one project is a request to go straight there, while starting
	// repohop plainly should always show what there is to choose from.
	var project model.Project
	if flagProject != "" {
		project, err = cfg.Resolve(flagProject)
		if err != nil {
			var unknown *config.UnknownProjectError
			if errors.As(err, &unknown) {
				return usageError{err}
			}
			return usageError{err}
		}
	}

	return tui.Run(cmd.Context(), cfg, project)
}
