package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate points the XDG environment at temporary directories so tests never
// read or write the developer's real config.
func isolate(t *testing.T) (configDir, stateDir string) {
	t.Helper()
	root := t.TempDir()
	configDir = filepath.Join(root, "config")
	stateDir = filepath.Join(root, "state")
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("XDG_STATE_HOME", stateDir)
	t.Setenv("HOME", filepath.Join(root, "home"))
	return configDir, stateDir
}

func write(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingConfigIsNotAnError(t *testing.T) {
	isolate(t)
	cfg, err := Load(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 0 {
		t.Errorf("Projects = %v, want none", cfg.Names())
	}
	if cfg.Settings != DefaultSettings() {
		t.Errorf("Settings = %+v, want defaults", cfg.Settings)
	}
}

func TestRepoSpecFormsAndPathResolution(t *testing.T) {
	configDir, _ := isolate(t)
	home := os.Getenv("HOME")
	write(t, filepath.Join(configDir, "repohop", "config.yaml"), `
version: 1
projects:
  - name: acme
    base: ~/src/acme
    repos:
      - api
      - web
      - path: ~/other/place/docs
        name: documentation
      - path: /absolute/worker
`)

	cfg, err := Load(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	project, ok := cfg.Project("acme")
	if !ok {
		t.Fatal("project acme not loaded")
	}

	want := []struct{ name, path string }{
		{"api", filepath.Join(home, "src", "acme", "api")},
		{"web", filepath.Join(home, "src", "acme", "web")},
		{"documentation", filepath.Join(home, "other", "place", "docs")},
		{"worker", filepath.FromSlash("/absolute/worker")},
	}
	if len(project.Repos) != len(want) {
		t.Fatalf("got %d repos, want %d", len(project.Repos), len(want))
	}
	for i, w := range want {
		got := project.Repos[i]
		if got.Name != w.name || got.Path != w.path {
			t.Errorf("repo %d = %q at %q, want %q at %q", i, got.Name, got.Path, w.name, w.path)
		}
	}
}

func TestEnvExpansionInPaths(t *testing.T) {
	configDir, _ := isolate(t)
	t.Setenv("ACME_SRC", filepath.Join(t.TempDir(), "checkouts"))
	write(t, filepath.Join(configDir, "repohop", "config.yaml"), `
projects:
  - name: acme
    base: $ACME_SRC
    repos: [api]
`)
	cfg, err := Load(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(os.Getenv("ACME_SRC"), "api")
	if got := cfg.Projects[0].Repos[0].Path; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestDirectoryConfigMergesAndWins(t *testing.T) {
	configDir, _ := isolate(t)
	write(t, filepath.Join(configDir, "repohop", "config.yaml"), `
defaults:
  concurrency: 4
projects:
  - name: acme
    base: /user/acme
    repos: [api]
  - name: personal
    repos: [/dev/thing]
`)

	work := t.TempDir()
	nested := filepath.Join(work, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(work, DirConfigName), `
defaults:
  pull: false
projects:
  - name: acme
    base: /team/acme
    repos: [api, web]
  - name: team-only
    repos: [/team/extra]
`)

	cfg, err := Load(Options{Dir: nested})
	if err != nil {
		t.Fatal(err)
	}

	// The directory config wins on the name collision, keeps the user's other
	// project, and appends its own.
	if got, want := strings.Join(cfg.Names(), ","), "acme,personal,team-only"; got != want {
		t.Errorf("projects = %q, want %q", got, want)
	}
	acme, _ := cfg.Project("acme")
	if len(acme.Repos) != 2 {
		t.Errorf("acme has %d repos, want the directory config's 2", len(acme.Repos))
	}
	if acme.Repos[0].Path != filepath.FromSlash("/team/acme/api") {
		t.Errorf("acme base = %q, want the directory config's base", acme.Repos[0].Path)
	}

	// Defaults merge key by key: the directory config sets pull, the user
	// config's concurrency survives.
	if cfg.Settings.Pull {
		t.Error("Pull = true, want the directory config's false")
	}
	if cfg.Settings.Concurrency != 4 {
		t.Errorf("Concurrency = %d, want the user config's 4", cfg.Settings.Concurrency)
	}
	if !cfg.Settings.Fetch {
		t.Error("Fetch = false, want the built-in default true")
	}
	if cfg.DirPath == "" {
		t.Error("DirPath is empty, want the discovered directory config")
	}
}

func TestExplicitPathSkipsDiscovery(t *testing.T) {
	configDir, _ := isolate(t)
	write(t, filepath.Join(configDir, "repohop", "config.yaml"), `
projects:
  - name: from-user
    repos: [/a]
`)
	work := t.TempDir()
	write(t, filepath.Join(work, DirConfigName), `
projects:
  - name: from-directory
    repos: [/b]
`)
	explicit := write(t, filepath.Join(t.TempDir(), "explicit.yaml"), `
projects:
  - name: explicit
    repos: [/c]
`)

	cfg, err := Load(Options{Path: explicit, Dir: work})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Names(); len(got) != 1 || got[0] != "explicit" {
		t.Errorf("projects = %v, want only the explicit file's", got)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"unsupported version", "version: 99\nprojects: []\n", "unsupported version"},
		{"project without a name", "projects:\n  - repos: [/a]\n", "has no name"},
		{"duplicate project", "projects:\n  - name: a\n    repos: [/x]\n  - name: a\n    repos: [/y]\n", "duplicate project"},
		{"malformed yaml", "projects: [oops\n", "yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			path := write(t, filepath.Join(t.TempDir(), "config.yaml"), tt.content)
			_, err := Load(Options{Path: path})
			if err == nil {
				t.Fatalf("Load() succeeded, want an error mentioning %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load() = %v, want an error mentioning %q", err, tt.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	isolate(t)
	path := write(t, filepath.Join(t.TempDir(), "config.yaml"), `
projects:
  - name: one
    repos: [/a]
  - name: two
    repos: [/b]
`)
	cfg, err := Load(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("explicit name", func(t *testing.T) {
		project, err := cfg.Resolve("two")
		if err != nil || project.Name != "two" {
			t.Fatalf("Resolve(two) = %q, %v", project.Name, err)
		}
	})

	t.Run("unknown name", func(t *testing.T) {
		_, err := cfg.Resolve("three")
		var unknown *UnknownProjectError
		if !errors.As(err, &unknown) {
			t.Fatalf("Resolve() = %v, want UnknownProjectError", err)
		}
	})

	t.Run("ambiguous without a selection", func(t *testing.T) {
		if _, err := cfg.Resolve(""); !errors.Is(err, ErrAmbiguousProject) {
			t.Fatalf("Resolve() = %v, want ErrAmbiguousProject", err)
		}
	})

	t.Run("remembered active project", func(t *testing.T) {
		if err := SaveState(State{ActiveProject: "two"}); err != nil {
			t.Fatal(err)
		}
		project, err := cfg.Resolve("")
		if err != nil || project.Name != "two" {
			t.Fatalf("Resolve() = %q, %v, want the remembered project", project.Name, err)
		}
	})

	t.Run("a stale remembered project falls through", func(t *testing.T) {
		if err := SaveState(State{ActiveProject: "gone"}); err != nil {
			t.Fatal(err)
		}
		if _, err := cfg.Resolve(""); !errors.Is(err, ErrAmbiguousProject) {
			t.Fatalf("Resolve() = %v, want ErrAmbiguousProject", err)
		}
	})
}

func TestResolveSingleProject(t *testing.T) {
	isolate(t)
	path := write(t, filepath.Join(t.TempDir(), "config.yaml"), "projects:\n  - name: only\n    repos: [/a]\n")
	cfg, err := Load(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	project, err := cfg.Resolve("")
	if err != nil || project.Name != "only" {
		t.Fatalf("Resolve() = %q, %v, want the single project", project.Name, err)
	}
}

func TestResolveNoProjects(t *testing.T) {
	isolate(t)
	cfg, err := Load(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Resolve(""); !errors.Is(err, ErrNoProjects) {
		t.Fatalf("Resolve() = %v, want ErrNoProjects", err)
	}
}

func TestStateRoundTrip(t *testing.T) {
	_, stateDir := isolate(t)
	state, err := LoadState()
	if err != nil || state.ActiveProject != "" {
		t.Fatalf("LoadState() on a fresh machine = %+v, %v", state, err)
	}
	if err := SaveState(State{ActiveProject: "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "repohop", "state.yaml")); err != nil {
		t.Fatalf("state file not written: %v", err)
	}
	state, err = LoadState()
	if err != nil || state.ActiveProject != "acme" {
		t.Fatalf("LoadState() = %+v, %v, want acme", state, err)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	isolate(t)
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	file := &File{Projects: []ProjectSpec{{
		Name:  "acme",
		Base:  "~/src/acme",
		Repos: []RepoSpec{{Path: "api"}, {Path: "/elsewhere/docs", Name: "docs"}},
	}}}
	if err := Save(path, file); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "version: 1") {
		t.Errorf("saved config has no version key:\n%s", data)
	}
	// The bare-string form survives a round trip; only the named entry expands.
	if !strings.Contains(string(data), "- api\n") {
		t.Errorf("simple repo entry was not written in the scalar form:\n%s", data)
	}

	cfg, err := Load(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 1 || len(cfg.Projects[0].Repos) != 2 {
		t.Fatalf("reloaded config = %+v", cfg.Projects)
	}
	if cfg.Projects[0].Repos[1].Name != "docs" {
		t.Errorf("display-name override lost in the round trip")
	}
}

func TestFindDirConfigWalksUp(t *testing.T) {
	root := t.TempDir()
	want := write(t, filepath.Join(root, DirConfigName), "projects: []\n")
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := FindDirConfig(deep); got != want {
		t.Errorf("FindDirConfig(deep) = %q, want %q", got, want)
	}

	// The nearest one wins.
	nearer := write(t, filepath.Join(root, "a", DirConfigName), "projects: []\n")
	if got := FindDirConfig(deep); got != nearer {
		t.Errorf("FindDirConfig(deep) = %q, want the nearest config %q", got, nearer)
	}
}

func TestAddProject(t *testing.T) {
	isolate(t)
	path := filepath.Join(t.TempDir(), "config.yaml")

	if err := AddProject(path, ProjectSpec{Name: "acme", Base: "~/src", Repos: []RepoSpec{{Path: "api"}}}); err != nil {
		t.Fatal(err)
	}
	if err := AddProject(path, ProjectSpec{Name: "other", Repos: []RepoSpec{{Path: "/b"}}}); err != nil {
		t.Fatal(err)
	}
	// The same name replaces rather than duplicating.
	if err := AddProject(path, ProjectSpec{Name: "acme", Base: "~/src", Repos: []RepoSpec{{Path: "api"}, {Path: "web"}}}); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.Names(), ","); got != "acme,other" {
		t.Fatalf("projects = %q, want acme,other", got)
	}
	acme, _ := cfg.Project("acme")
	if len(acme.Repos) != 2 {
		t.Errorf("acme has %d repos, want the rewritten 2", len(acme.Repos))
	}
}
