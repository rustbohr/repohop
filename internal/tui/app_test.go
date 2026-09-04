package tui

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/rustbohr/repohop/internal/config"
	"github.com/rustbohr/repohop/internal/model"
)

// The screen-stack tests drive the real program against real repositories.
// Golden-file rendering is deliberately avoided: too brittle for the value.

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", "LC_ALL=C",
		"GIT_AUTHOR_NAME=repohop test", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=repohop test", "GIT_COMMITTER_EMAIL=test@example.invalid",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// testProject creates two repositories, one of them carrying an extra branch.
func testProject(t *testing.T) model.Project {
	t.Helper()
	root := t.TempDir()
	var repos []model.Repo
	for i, name := range []string{"api", "web"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "-c", "init.defaultBranch=master", "init", "-q", ".")
		gitRun(t, dir, "config", "user.name", "repohop test")
		gitRun(t, dir, "config", "user.email", "test@example.invalid")
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "-A")
		gitRun(t, dir, "commit", "-q", "--no-gpg-sign", "-m", "initial commit")
		if i == 0 {
			gitRun(t, dir, "branch", "feat/checkout")
		}
		repos = append(repos, model.Repo{Name: name, Path: dir})
	}
	return model.Project{Name: "acme", Repos: repos}
}

func testConfig() *config.Config {
	return &config.Config{Settings: config.DefaultSettings(), UserPath: "/dev/null/config.yaml"}
}

// ui wraps a running program with an accumulating view of its output: the
// renderer only writes when the frame changes, so each read has to be kept.
type ui struct {
	tm   *teatest.TestModel
	seen bytes.Buffer
}

func start(t *testing.T, app tea.Model) *ui {
	t.Helper()
	return &ui{tm: teatest.NewTestModel(t, app, teatest.WithInitialTermSize(120, 30))}
}

// waitFor blocks until want has appeared in the output since the last forget.
func (u *ui) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := io.Copy(&u.seen, u.tm.Output()); err != nil {
			t.Fatalf("reading program output: %v", err)
		}
		if bytes.Contains(u.seen.Bytes(), []byte(want)) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never saw %q. Output so far:\n%s", want, u.seen.String())
}

// forget drops what has been rendered so far, so a later assertion cannot be
// satisfied by a stale frame.
func (u *ui) forget() {
	_, _ = io.Copy(&u.seen, u.tm.Output())
	u.seen.Reset()
}

func (u *ui) press(key string) {
	u.tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

func (u *ui) send(msg tea.Msg) { u.tm.Send(msg) }

func (u *ui) quit(t *testing.T) {
	t.Helper()
	u.tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	u.tm.WaitFinished(t, teatest.WithFinalTimeout(10*time.Second))
}

func TestDashboardLoadsRowsAndOpensPicker(t *testing.T) {
	u := start(t, New(context.Background(), testConfig(), testProject(t)))

	// Rows fill in from the concurrent status reads.
	u.waitFor(t, "master")
	u.waitFor(t, "clean")

	// s opens the picker, which enumerates branches across both repositories.
	u.forget()
	u.press("s")
	u.waitFor(t, "pick a branch")
	u.waitFor(t, "feat/checkout")

	// Typing filters, and the preview says how many repositories carry it.
	u.forget()
	u.tm.Type("feat")
	u.waitFor(t, "1/2")

	// esc pops back to the dashboard.
	u.forget()
	u.send(tea.KeyMsg{Type: tea.KeyEsc})
	u.waitFor(t, "dashboard")

	u.quit(t)
}

func TestHelpOverlayToggles(t *testing.T) {
	u := start(t, New(context.Background(), testConfig(), testProject(t)))
	u.waitFor(t, "REPO")

	u.forget()
	u.press("?")
	u.waitFor(t, "abort immediately")

	u.forget()
	u.send(tea.KeyMsg{Type: tea.KeyEsc})
	u.waitFor(t, "LAST COMMIT")

	u.quit(t)
}

func TestProjectListIsShownWhenNoProjectIsResolved(t *testing.T) {
	cfg := testConfig()
	cfg.Projects = []model.Project{
		{Name: "acme", Repos: []model.Repo{{Name: "api", Path: "/r/api"}}},
		{Name: "side", Repos: []model.Repo{{Name: "thing", Path: "/r/thing"}}},
	}
	u := start(t, New(context.Background(), cfg, model.Project{}))

	u.waitFor(t, "acme")
	u.waitFor(t, "side")
	u.waitFor(t, "1 repo")

	u.quit(t)
}

func TestFirstRunEmptyStatePointsAtSetup(t *testing.T) {
	u := start(t, New(context.Background(), testConfig(), model.Project{}))
	u.waitFor(t, "No projects configured yet")
	u.waitFor(t, "scan a directory")

	u.forget()
	u.press("n")
	u.waitFor(t, "Which directory holds your repositories?")

	u.quit(t)
}

// dirtyProject builds two repositories that both carry feat/checkout, one of
// them with an uncommitted change.
func dirtyProject(t *testing.T) model.Project {
	t.Helper()
	project := testProject(t)
	gitRun(t, project.Repos[1].Path, "branch", "feat/checkout")
	if err := os.WriteFile(filepath.Join(project.Repos[1].Path, "README.md"), []byte("work in progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return project
}

func TestSwitchOffersAChoiceAboutDirtyRepositories(t *testing.T) {
	project := dirtyProject(t)
	u := start(t, New(context.Background(), testConfig(), project))
	u.waitFor(t, "dirty")

	u.forget()
	u.press("s")
	u.waitFor(t, "feat/checkout")

	// Pick the branch: the preflight finds the dirty repository and asks.
	u.forget()
	u.send(tea.KeyMsg{Type: tea.KeyEnter})
	u.waitFor(t, "has local changes")
	u.waitFor(t, "skip them and switch the rest")

	// Take the default, skipping the dirty repository.
	u.forget()
	u.send(tea.KeyMsg{Type: tea.KeyEnter})
	u.waitFor(t, "dirty, skipped")

	if got := currentBranch(t, project.Repos[0].Path); got != "feat/checkout" {
		t.Errorf("api is on %q, want feat/checkout", got)
	}
	if got := currentBranch(t, project.Repos[1].Path); got != "master" {
		t.Errorf("web moved to %q despite being dirty", got)
	}

	u.quit(t)
}

func TestSwitchStashesWhenChosen(t *testing.T) {
	project := dirtyProject(t)
	u := start(t, New(context.Background(), testConfig(), project))
	u.waitFor(t, "dirty")

	u.press("s")
	u.waitFor(t, "feat/checkout")
	u.send(tea.KeyMsg{Type: tea.KeyEnter})
	u.waitFor(t, "has local changes")

	// Move to the stash option and take it.
	u.forget()
	u.send(tea.KeyMsg{Type: tea.KeyDown})
	u.send(tea.KeyMsg{Type: tea.KeyEnter})
	u.waitFor(t, "press u to restore")

	for _, repo := range project.Repos {
		if got := currentBranch(t, repo.Path); got != "feat/checkout" {
			t.Errorf("%s is on %q, want feat/checkout", repo.Name, got)
		}
	}

	// Restoring puts the change back.
	u.forget()
	u.press("u")
	u.waitFor(t, "1 stash restored")

	content, err := os.ReadFile(filepath.Join(project.Repos[1].Path, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "work in progress\n" {
		t.Errorf("restored file = %q, want the stashed content", content)
	}

	u.quit(t)
}

func TestDashboardSelectionNarrowsAnAction(t *testing.T) {
	project := testProject(t)
	u := start(t, New(context.Background(), testConfig(), project))
	u.waitFor(t, "master")

	// Select only the first repository, then open the picker: the preview is
	// now over one repository, not two.
	u.forget()
	u.send(tea.KeyMsg{Type: tea.KeySpace})
	u.waitFor(t, "1 repository selected")

	u.forget()
	u.press("s")
	u.waitFor(t, "1/1")

	u.quit(t)
}

func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func TestFetchFromTheDashboardReportsPerRepository(t *testing.T) {
	u := start(t, New(context.Background(), testConfig(), testProject(t)))
	u.waitFor(t, "master")

	// These repositories have no remote, which is reported rather than failing.
	u.forget()
	u.press("f")
	u.waitFor(t, "no remote configured")

	u.forget()
	u.send(tea.KeyMsg{Type: tea.KeyEsc})
	u.waitFor(t, "dashboard")

	u.quit(t)
}
