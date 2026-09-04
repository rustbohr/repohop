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

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// An unresolved project is not an error here: the TUI opens on the project
	// list, which also carries the first-run empty state.
	project, err := cfg.Resolve(flagProject)
	if err != nil {
		var unknown *config.UnknownProjectError
		if errors.As(err, &unknown) {
			return usageError{err}
		}
		project = model.Project{}
	}

	return tui.Run(cmd.Context(), cfg, project)
}
