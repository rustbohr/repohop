package cli

import (
	"text/tabwriter"

	"github.com/rustbohr/repohop/internal/config"
	"github.com/rustbohr/repohop/internal/logging"
	"github.com/rustbohr/repohop/internal/scan"
	"github.com/spf13/cobra"
)

func newProjectsCmd() *cobra.Command {
	cmd := newProjectsListCmd()
	cmd.AddCommand(newProjectsAddCmd(), newProjectsRemoveCmd(), newProjectsUseCmd())
	return cmd
}

func newProjectsListCmd() *cobra.Command {
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
			if logPath := logging.Path(); logPath != "" {
				fmtLine(tw, "log", logPath, existsNote(logPath))
			}
			return tw.Flush()
		},
	})
	return cmd
}

// projectTarget is the file project edits are written to: the explicit
// --config file when there is one, otherwise the user's own config. A project
// that came from a committed .repohop.yaml is never rewritten from here.
func projectTarget(cfg *config.Config, name string) (string, error) {
	if name == "" {
		return cfg.UserPath, nil
	}
	project, ok := cfg.Project(name)
	if ok && project.Source != cfg.UserPath {
		return "", usagef("project %q is defined in %s; edit that file", name, project.Source)
	}
	return cfg.UserPath, nil
}

func newProjectsAddCmd() *cobra.Command {
	var (
		base      string
		repoPaths []string
		scanRoot  string
		depth     int
	)

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add or replace a project in the config file",
		Long: "Add or replace a project in the config file.\n\n" +
			"Give the repositories with --repo, or point --scan at a directory to take\n" +
			"every git repository underneath it. A project of the same name is replaced.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			target, err := projectTarget(cfg, name)
			if err != nil {
				return err
			}

			spec := config.ProjectSpec{Name: name, Base: base}
			for _, path := range repoPaths {
				spec.Repos = append(spec.Repos, config.RepoSpec{Path: path})
			}

			if scanRoot != "" {
				root := config.ExpandPath(scanRoot)
				found, err := scan.Find(cmd.Context(), scan.Options{Root: root, Depth: depth})
				if err != nil {
					return usageError{err}
				}
				if len(found) == 0 {
					return usagef("no git repositories under %s", root)
				}
				if spec.Base == "" {
					spec.Base = scanRoot
					for _, repo := range found {
						spec.Repos = append(spec.Repos, config.RepoSpec{Path: repo.Rel})
					}
				} else {
					for _, repo := range found {
						spec.Repos = append(spec.Repos, config.RepoSpec{Path: repo.Path})
					}
				}
			}

			if len(spec.Repos) == 0 {
				return usagef("a project needs repositories: pass --repo or --scan")
			}
			if err := config.AddProject(target, spec); err != nil {
				return usageError{err}
			}

			cmd.Printf("wrote project %q with %d repositories to %s\n", name, len(spec.Repos), shortenPath(target))
			return nil
		},
	}

	cmd.Flags().StringVar(&base, "base", "", "directory the repo entries are relative to")
	cmd.Flags().StringArrayVar(&repoPaths, "repo", nil, "a repository path (repeatable)")
	cmd.Flags().StringVar(&scanRoot, "scan", "", "take every git repository under this directory")
	cmd.Flags().IntVar(&depth, "depth", scan.DefaultDepth, "how far below --scan to look")
	return cmd
}

func newProjectsRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove"},
		Short:   "Remove a project from the config file",
		Long: "Remove a project from the config file.\n\n" +
			"Only the configuration entry is removed; the repositories on disk are\n" +
			"never touched.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if _, ok := cfg.Project(name); !ok {
				return usageError{&config.UnknownProjectError{Name: name, Known: cfg.Names()}}
			}
			target, err := projectTarget(cfg, name)
			if err != nil {
				return err
			}
			if err := config.RemoveProject(target, name); err != nil {
				return usageError{err}
			}

			if state, err := config.LoadState(); err == nil && state.ActiveProject == name {
				_ = config.SaveState(config.State{})
			}
			cmd.Printf("removed project %q from %s\n", name, shortenPath(target))
			return nil
		},
	}
	return cmd
}

func newProjectsUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Remember a project as the active one",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if _, ok := cfg.Project(name); !ok {
				return usageError{&config.UnknownProjectError{Name: name, Known: cfg.Names()}}
			}
			if err := config.SaveState(config.State{ActiveProject: name}); err != nil {
				return usageError{err}
			}
			cmd.Printf("active project is now %q\n", name)
			return nil
		},
	}
}
