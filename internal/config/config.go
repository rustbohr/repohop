// Package config loads repohop's configuration: a user-level file, optionally
// overlaid with a per-directory one found by walking up from the working
// directory.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/rustbohr/repohop/internal/model"
	"gopkg.in/yaml.v3"
)

// SchemaVersion is the config format version. Present from day one so the
// format can evolve.
const SchemaVersion = 1

// ErrNoConfig means no config file was found in any location.
var ErrNoConfig = errors.New("no configuration found")

// File is the on-disk schema.
type File struct {
	Version  int           `yaml:"version"`
	Defaults Defaults      `yaml:"defaults,omitempty"`
	Projects []ProjectSpec `yaml:"projects"`

	// Path is where the file was read from; not part of the schema.
	Path string `yaml:"-"`
}

// Defaults are the behavioural knobs. Pointers so an unset key is
// distinguishable from an explicit false.
type Defaults struct {
	Fetch       *bool `yaml:"fetch,omitempty"`
	Pull        *bool `yaml:"pull,omitempty"`
	Concurrency int   `yaml:"concurrency,omitempty"`
}

// ProjectSpec is a project as written in the file.
type ProjectSpec struct {
	Name  string     `yaml:"name"`
	Base  string     `yaml:"base,omitempty"`
	Repos []RepoSpec `yaml:"repos"`
}

// RepoSpec is a repository entry: either a bare string joined to the project's
// base, or a mapping with an explicit path and an optional display name.
type RepoSpec struct {
	Path string `yaml:"path"`
	Name string `yaml:"name,omitempty"`
}

// UnmarshalYAML accepts both the scalar and the mapping form.
func (r *RepoSpec) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		return node.Decode(&r.Path)
	}
	type plain RepoSpec // avoid recursing into this method
	var p plain
	if err := node.Decode(&p); err != nil {
		return err
	}
	*r = RepoSpec(p)
	return nil
}

// MarshalYAML writes the scalar form whenever there is nothing to add, so a
// config repohop writes back stays as readable as a hand-written one.
func (r RepoSpec) MarshalYAML() (any, error) {
	if r.Name == "" {
		return r.Path, nil
	}
	type plain RepoSpec
	return plain(r), nil
}

// Settings are the defaults with every value resolved.
type Settings struct {
	Fetch       bool
	Pull        bool
	Concurrency int
}

// DefaultSettings are what repohop does when the config says nothing.
func DefaultSettings() Settings {
	return Settings{Fetch: true, Pull: true, Concurrency: DefaultConcurrency()}
}

// DefaultConcurrency bounds the read-only worker pool.
func DefaultConcurrency() int {
	if n := runtime.NumCPU() * 2; n < 8 {
		return n
	}
	return 8
}

func (s Settings) merge(d Defaults) Settings {
	if d.Fetch != nil {
		s.Fetch = *d.Fetch
	}
	if d.Pull != nil {
		s.Pull = *d.Pull
	}
	if d.Concurrency > 0 {
		s.Concurrency = d.Concurrency
	}
	return s
}

// Config is the merged, resolved configuration the rest of repohop uses.
type Config struct {
	Settings Settings
	Projects []model.Project

	// UserPath is the user-level config file, whether or not it exists.
	UserPath string
	// DirPath is the directory config that was found, or empty.
	DirPath string
	// Loaded lists the files that actually contributed, in precedence order.
	Loaded []string
}

// Project looks a project up by name.
func (c *Config) Project(name string) (model.Project, bool) {
	for _, p := range c.Projects {
		if p.Name == name {
			return p, true
		}
	}
	return model.Project{}, false
}

// Names lists the configured project names in order.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Projects))
	for _, p := range c.Projects {
		names = append(names, p.Name)
	}
	return names
}

// Options control config discovery.
type Options struct {
	// Path, when set, is used alone: no discovery, no merging.
	Path string
	// Dir is where the ancestor walk for a directory config starts; empty
	// means the current working directory.
	Dir string
}

// Load discovers and merges configuration. A missing file is not an error —
// the caller decides whether an empty config means "run setup".
func Load(opts Options) (*Config, error) {
	userPath, err := UserConfigPath()
	if err != nil {
		return nil, err
	}
	cfg := &Config{Settings: DefaultSettings(), UserPath: userPath}

	if opts.Path != "" {
		file, err := readFile(opts.Path)
		if err != nil {
			return nil, err
		}
		cfg.UserPath = opts.Path
		return cfg, cfg.apply(file)
	}

	if file, err := readFile(userPath); err == nil {
		if err := cfg.apply(file); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	dir := opts.Dir
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return nil, err
		}
	}
	if found := FindDirConfig(dir); found != "" {
		// A directory config never merges with itself when it is also the
		// user's config file.
		if found != userPath {
			cfg.DirPath = found
			file, err := readFile(found)
			if err != nil {
				return nil, err
			}
			if err := cfg.apply(file); err != nil {
				return nil, err
			}
		}
	}
	return cfg, nil
}

// apply merges one file into the config. Later files win: their defaults
// override, and their projects replace same-named earlier ones in place.
func (c *Config) apply(file *File) error {
	if file.Version != 0 && file.Version != SchemaVersion {
		return fmt.Errorf("%s: unsupported version %d (this repohop understands version %d)",
			file.Path, file.Version, SchemaVersion)
	}
	c.Settings = c.Settings.merge(file.Defaults)

	seen := map[string]bool{}
	for _, spec := range file.Projects {
		project, err := resolveProject(spec, file.Path)
		if err != nil {
			return fmt.Errorf("%s: %w", file.Path, err)
		}
		if seen[project.Name] {
			return fmt.Errorf("%s: duplicate project %q", file.Path, project.Name)
		}
		seen[project.Name] = true

		if i := indexOf(c.Projects, project.Name); i >= 0 {
			c.Projects[i] = project
		} else {
			c.Projects = append(c.Projects, project)
		}
	}
	c.Loaded = append(c.Loaded, file.Path)
	return nil
}

func indexOf(projects []model.Project, name string) int {
	for i, p := range projects {
		if p.Name == name {
			return i
		}
	}
	return -1
}

func resolveProject(spec ProjectSpec, path string) (model.Project, error) {
	if spec.Name == "" {
		return model.Project{}, errors.New("a project has no name")
	}
	configDir := filepath.Dir(path)
	project := model.Project{Name: spec.Name, Base: spec.Base, Source: path}

	for _, entry := range spec.Repos {
		if entry.Path == "" {
			return model.Project{}, fmt.Errorf("project %q: a repo entry has no path", spec.Name)
		}
		resolved := resolvePath(entry.Path, spec.Base, configDir)
		name := entry.Name
		if name == "" {
			name = filepath.Base(resolved)
		}
		project.Repos = append(project.Repos, model.Repo{Name: name, Path: resolved})
	}
	return project, nil
}

func readFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file File
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	file.Path = path
	return &file, nil
}

// Save writes a config file, creating the parent directory as needed.
func Save(path string, file *File) error {
	if file.Version == 0 {
		file.Version = SchemaVersion
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o644)
}

// writeFileAtomic writes via a temporary file in the same directory, so an
// interrupted write can never leave a truncated config behind.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

var (
	// ErrNoProjects means the configuration defines no projects at all.
	ErrNoProjects = errors.New("no projects configured")
	// ErrAmbiguousProject means several projects exist and none was chosen.
	ErrAmbiguousProject = errors.New("several projects configured and none selected")
)

// UnknownProjectError names a project that is not in the configuration.
type UnknownProjectError struct {
	Name  string
	Known []string
}

func (e *UnknownProjectError) Error() string {
	if len(e.Known) == 0 {
		return fmt.Sprintf("unknown project %q", e.Name)
	}
	return fmt.Sprintf("unknown project %q (configured: %v)", e.Name, e.Known)
}

// Resolve picks the project to act on: the explicitly named one, else the
// remembered active project, else the only project there is.
func (c *Config) Resolve(name string) (model.Project, error) {
	if name != "" {
		project, ok := c.Project(name)
		if !ok {
			return model.Project{}, &UnknownProjectError{Name: name, Known: c.Names()}
		}
		return project, nil
	}
	if len(c.Projects) == 0 {
		return model.Project{}, ErrNoProjects
	}
	if state, err := LoadState(); err == nil && state.ActiveProject != "" {
		if project, ok := c.Project(state.ActiveProject); ok {
			return project, nil
		}
	}
	if len(c.Projects) == 1 {
		return c.Projects[0], nil
	}
	return model.Project{}, ErrAmbiguousProject
}
