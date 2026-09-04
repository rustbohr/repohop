package cli

import (
	"errors"
	"text/tabwriter"

	"github.com/rustbohr/repohop/internal/ops"
	"github.com/spf13/cobra"
)

func newSwitchCmd() *cobra.Command {
	var (
		only    []string
		noFetch bool
		noPull  bool
		stash   bool
	)

	cmd := &cobra.Command{
		Use:   "switch <branch>",
		Short: "Put every repository onto one branch",
		Long: "Put every repository onto one branch.\n\n" +
			"A repository is only switched if the branch already exists locally or on\n" +
			"origin; repohop never creates a branch. Repositories with local changes\n" +
			"are skipped unless --stash is given.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			branch := args[0]
			cfg, project, err := activeProject(cmd.Context())
			if err != nil {
				return err
			}
			repos, err := selectRepos(project, only)
			if err != nil {
				return err
			}

			runner := ops.New(cfg.Settings.Concurrency)
			ctx := cmd.Context()

			if cfg.Settings.Fetch && !noFetch {
				for _, result := range runner.Fetch(ctx, repos, nil) {
					if result.Err != nil {
						cmd.PrintErrf("warning: %s: fetch failed: %v\n", result.Repo.Name, result.Err)
					}
				}
			}

			opts := ops.SwitchOptions{
				Branch: branch,
				Pull:   cfg.Settings.Pull && !noPull,
				Dirty:  ops.DirtySkip,
			}
			if stash {
				opts.Dirty = ops.DirtyStash
			}

			results := runner.Switch(ctx, repos, opts, nil)
			writeSwitchSummary(cmd, results)

			if n := ops.SwitchFailures(results); n > 0 {
				return partialError{errors.New(itoa(n) + " of " + itoa(len(results)) + " repositories did not switch")}
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&only, "only", nil, "act on these repositories only")
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "skip the fetch before switching")
	cmd.Flags().BoolVar(&noPull, "no-pull", false, "skip the fast-forward after switching")
	cmd.Flags().BoolVar(&stash, "stash", false, "stash local changes instead of skipping the repository")
	return cmd
}

// writeSwitchSummary prints the transition table: the single most useful piece
// of output the tool has.
func writeSwitchSummary(cmd *cobra.Command, results []ops.SwitchResult) {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmtLine(tw, "REPO", "OLD BRANCH", "", "NEW BRANCH", "")
	for _, result := range results {
		fmtLine(tw, result.Repo.Name, result.OldBranch, "→", result.NewBranch, summaryNote(result))
	}
	tw.Flush()
}

func summaryNote(result ops.SwitchResult) string {
	var note string
	switch result.Outcome {
	case ops.OutcomeSwitched, ops.OutcomeUnchanged:
		note = result.Note
	case ops.OutcomeFailed:
		note = "failed: " + result.Err.Error()
	default:
		note = result.Outcome.String()
		if result.Note != "" {
			note += ", " + result.Note
		}
	}
	if note == "" {
		return ""
	}
	return "(" + note + ")"
}
