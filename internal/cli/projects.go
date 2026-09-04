package cli

import (
	"text/tabwriter"

	"github.com/rustbohr/repohop/internal/config"
	"github.com/spf13/cobra"
)

func newProjectsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "projects",
		Short: "List the configured projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if len(cfg.Projects) == 0 {
				cmd.Printf("No projects configured. Write %s or run repohop to set one up.\n", cfg.UserPath)
				return nil
			}

			active := ""
			if project, err := cfg.Resolve(flagProject); err == nil {
				active = project.Name
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmtLine(tw, "", "PROJECT", "REPOS", "SOURCE")
			for _, project := range cfg.Projects {
				marker := " "
				if project.Name == active {
					marker = "*"
				}
				fmtLine(tw, marker, project.Name, itoa(len(project.Repos)), shortenPath(project.Source))
			}
			return tw.Flush()
		},
	}
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect repohop's configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the configuration file locations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			statePath, err := config.StatePath()
			if err != nil {
				return usageError{err}
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmtLine(tw, "user", cfg.UserPath, existsNote(cfg.UserPath))
			if cfg.DirPath != "" {
				fmtLine(tw, "directory", cfg.DirPath, "")
			}
			fmtLine(tw, "state", statePath, existsNote(statePath))
			return tw.Flush()
		},
	})
	return cmd
}
