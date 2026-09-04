package cli

import (
	"context"
	"errors"

	"github.com/rustbohr/repohop/internal/config"
	"github.com/rustbohr/repohop/internal/git"
	"github.com/rustbohr/repohop/internal/model"
)

// requireGit fails once, clearly, when git is missing or too old, rather than
// letting every repository fail separately.
func requireGit(ctx context.Context) error {
	if err := git.Default.Require(ctx); err != nil {
		if errors.Is(err, git.ErrNotFound) {
			return usagef("git was not found on PATH; repohop drives the real git binary")
		}
		return usageError{err}
	}
	return nil
}

// loadConfig loads configuration honouring the global --config flag.
func loadConfig() (*config.Config, error) {
	cfg, err := config.Load(config.Options{Path: flagConfig})
	if err != nil {
		return nil, usageError{err}
	}
	return cfg, nil
}

// activeProject loads the configuration and resolves the project to act on,
// turning the "nothing configured yet" and "which one?" cases into advice
// rather than a bare error.
func activeProject(ctx context.Context) (*config.Config, model.Project, error) {
	if err := requireGit(ctx); err != nil {
		return nil, model.Project{}, err
	}
	cfg, err := loadConfig()
	if err != nil {
		return nil, model.Project{}, err
	}
	project, err := cfg.Resolve(flagProject)
	if err == nil {
		return cfg, project, nil
	}
	switch {
	case errors.Is(err, config.ErrNoProjects):
		return nil, model.Project{}, usagef(
			"no projects configured yet\n"+
				"  run repohop without arguments to set one up, or write %s", cfg.UserPath)
	case errors.Is(err, config.ErrAmbiguousProject):
		return nil, model.Project{}, usagef(
			"several projects configured; pick one with --project\n  %v", cfg.Names())
	default:
		return nil, model.Project{}, usageError{err}
	}
}

// selectRepos narrows a project to the repositories named in --only.
func selectRepos(project model.Project, only []string) ([]model.Repo, error) {
	if len(only) == 0 {
		return project.Repos, nil
	}
	repos := make([]model.Repo, 0, len(only))
	for _, name := range only {
		repo, ok := project.Repo(name)
		if !ok {
			return nil, usagef("project %q has no repository %q", project.Name, name)
		}
		repos = append(repos, repo)
	}
	return repos, nil
}
