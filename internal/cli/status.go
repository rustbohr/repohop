package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/rustbohr/repohop/internal/model"
	"github.com/rustbohr/repohop/internal/ops"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var asJSON bool
	var only []string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show every repository's branch and state",
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

			states := ops.New(cfg.Settings.Concurrency).Status(cmd.Context(), repos)
			if asJSON {
				if err := writeStatusJSON(cmd, project, states); err != nil {
					return err
				}
			} else {
				writeStatusTable(cmd, states)
			}

			for _, state := range states {
				if !state.OK() {
					return partialError{errors.New("some repositories could not be read")}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	cmd.Flags().StringSliceVar(&only, "only", nil, "act on these repositories only")
	return cmd
}

func writeStatusTable(cmd *cobra.Command, states []model.RepoState) {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmtLine(tw, "REPO", "BRANCH", "STATE", "SYNC", "LAST COMMIT")
	for _, state := range states {
		if !state.OK() {
			fmtLine(tw, state.Repo.Name, "—", stateError(state.Err), "—", "—")
			continue
		}
		fmtLine(tw, state.Repo.Name, state.Status.Ref(), dirtyLabel(state.Status.Dirty), syncLabel(state), lastCommit(state))
	}
	tw.Flush()
}

func dirtyLabel(dirty bool) string {
	if dirty {
		return "dirty"
	}
	return "clean"
}

func syncLabel(state model.RepoState) string {
	st := state.Status
	if st.Upstream == "" {
		return "—"
	}
	if st.Ahead == 0 && st.Behind == 0 {
		return "="
	}
	return fmt.Sprintf("↑%d ↓%d", st.Ahead, st.Behind)
}

func lastCommit(state model.RepoState) string {
	if state.Status.LastCommit.IsZero() {
		return "—"
	}
	return model.Age(time.Since(state.Status.LastCommit))
}

func stateError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// statusJSON is the --json shape. Kept explicit so the domain types can change
// without silently changing the output contract.
type statusJSON struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Branch     string `json:"branch,omitempty"`
	Detached   bool   `json:"detached"`
	Unborn     bool   `json:"unborn"`
	Head       string `json:"head,omitempty"`
	Dirty      bool   `json:"dirty"`
	Upstream   string `json:"upstream,omitempty"`
	Ahead      int    `json:"ahead"`
	Behind     int    `json:"behind"`
	LastCommit string `json:"last_commit,omitempty"`
	Error      string `json:"error,omitempty"`
}

func writeStatusJSON(cmd *cobra.Command, project model.Project, states []model.RepoState) error {
	rows := make([]statusJSON, 0, len(states))
	for _, state := range states {
		row := statusJSON{
			Name:     state.Repo.Name,
			Path:     state.Repo.Path,
			Branch:   state.Status.Branch,
			Detached: state.Status.Detached,
			Unborn:   state.Status.Unborn,
			Head:     state.Status.Head,
			Dirty:    state.Status.Dirty,
			Upstream: state.Status.Upstream,
			Ahead:    state.Status.Ahead,
			Behind:   state.Status.Behind,
			Error:    stateError(state.Err),
		}
		if !state.Status.LastCommit.IsZero() {
			row.LastCommit = state.Status.LastCommit.Format(time.RFC3339)
		}
		rows = append(rows, row)
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		Project string       `json:"project"`
		Repos   []statusJSON `json:"repos"`
	}{Project: project.Name, Repos: rows})
}
