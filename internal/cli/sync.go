package cli

import (
	"errors"
	"text/tabwriter"

	"github.com/rustbohr/repohop/internal/ops"
	"github.com/spf13/cobra"
)

func newFetchCmd() *cobra.Command {
	var only []string
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Fetch every repository, pruning deleted remote branches",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, project, err := activeProject(cmd.Context())
			if err != nil {
				return err
			}
			repos, err := selectRepos(project, only)
			if err != nil {
				return err
			}
			results := ops.New(cfg.Settings.Concurrency).Fetch(cmd.Context(), repos, nil)
			return reportOpResults(cmd, results, "fetched")
		},
	}
	cmd.Flags().StringSliceVar(&only, "only", nil, "act on these repositories only")
	return cmd
}

func newPullCmd() *cobra.Command {
	var only []string
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Fast-forward every repository's current branch",
		Long: "Fast-forward every repository's current branch.\n\n" +
			"A branch that has diverged from its upstream is reported and left alone;\n" +
			"repohop never merges, rebases or forces.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, project, err := activeProject(cmd.Context())
			if err != nil {
				return err
			}
			repos, err := selectRepos(project, only)
			if err != nil {
				return err
			}
			results := ops.New(cfg.Settings.Concurrency).Pull(cmd.Context(), repos, nil)
			return reportOpResults(cmd, results, "pulled")
		},
	}
	cmd.Flags().StringSliceVar(&only, "only", nil, "act on these repositories only")
	return cmd
}

// reportOpResults prints one row per repository and maps failures onto the
// partial-failure exit code.
func reportOpResults(cmd *cobra.Command, results []ops.OpResult, verb string) error {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmtLine(tw, "REPO", "RESULT")
	for _, result := range results {
		switch {
		case result.Err != nil:
			fmtLine(tw, result.Repo.Name, "failed: "+result.Err.Error())
		case result.Note != "":
			fmtLine(tw, result.Repo.Name, result.Note)
		default:
			fmtLine(tw, result.Repo.Name, verb)
		}
	}
	tw.Flush()

	if n := ops.Failures(results); n > 0 {
		return partialError{errors.New(itoa(n) + " of " + itoa(len(results)) + " repositories failed")}
	}
	return nil
}
