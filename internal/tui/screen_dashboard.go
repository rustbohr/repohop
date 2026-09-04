package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rustbohr/repohop/internal/git"
	"github.com/rustbohr/repohop/internal/model"
	"github.com/rustbohr/repohop/internal/task"
)

// dashboard is the home screen: one row per repository, filled in as the
// concurrent status reads land.
type dashboard struct {
	sh      *shared
	rows    []dashboardRow
	cursor  int
	offset  int
	loading int // repositories still to report
	spin    spinner.Model
}

type dashboardRow struct {
	state    model.RepoState
	loaded   bool
	selected bool
}

// Messages for the streaming status load.
type (
	statusRowMsg struct {
		result task.Result[git.Status]
		ch     <-chan task.Result[git.Status]
	}
	statusDoneMsg struct{}
)

func newDashboard(sh *shared) *dashboard {
	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	spin.Style = sh.theme.Spinner

	d := &dashboard{sh: sh, spin: spin}
	d.reset()
	return d
}

// reset rebuilds the rows from the project, preserving nothing: it is called
// on load and on refresh.
func (d *dashboard) reset() {
	selected := map[string]bool{}
	for _, row := range d.rows {
		if row.selected {
			selected[row.state.Repo.Path] = true
		}
	}

	d.rows = make([]dashboardRow, 0, len(d.sh.project.Repos))
	for _, repo := range d.sh.project.Repos {
		d.rows = append(d.rows, dashboardRow{
			state:    model.RepoState{Repo: repo},
			selected: selected[repo.Path],
		})
	}
	d.loading = len(d.rows)
	d.cursor = min(d.cursor, max(len(d.rows)-1, 0))
}

func (d *dashboard) Init() tea.Cmd {
	return tea.Batch(d.load(), d.spin.Tick)
}

// load starts the concurrent status read and pumps its results into the
// message loop.
func (d *dashboard) load() tea.Cmd {
	d.reset()
	repos := make([]model.Repo, 0, len(d.rows))
	for _, row := range d.rows {
		repos = append(repos, row.state.Repo)
	}
	ch := d.sh.runner.StatusStream(d.sh.ctx, repos)
	return waitForStatus(ch)
}

func waitForStatus(ch <-chan task.Result[git.Status]) tea.Cmd {
	return func() tea.Msg {
		result, ok := <-ch
		if !ok {
			return statusDoneMsg{}
		}
		return statusRowMsg{result: result, ch: ch}
	}
}

func (d *dashboard) Title() string { return "dashboard" }

func (d *dashboard) Hints() []Hint {
	return []Hint{
		{"space", "select"},
		{"a", "all"},
		{"s", "switch"},
		{"f", "fetch"},
		{"p", "pull"},
		{"r", "refresh"},
		{"e", "edit project"},
		{"q", "quit"},
	}
}

func (d *dashboard) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case statusRowMsg:
		if msg.result.Index < len(d.rows) {
			d.rows[msg.result.Index].state = model.RepoState{
				Repo:   msg.result.Repo,
				Status: msg.result.Value,
				Err:    msg.result.Err,
			}
			d.rows[msg.result.Index].loaded = true
			d.loading--
		}
		return d, waitForStatus(msg.ch)

	case statusDoneMsg:
		d.loading = 0
		return d, nil

	case refreshMsg:
		return d, d.load()

	case spinner.TickMsg:
		if d.loading == 0 {
			return d, nil
		}
		var cmd tea.Cmd
		d.spin, cmd = d.spin.Update(msg)
		return d, cmd

	case tea.KeyMsg:
		return d.key(msg)
	}
	return d, nil
}

func (d *dashboard) key(msg tea.KeyMsg) (screen, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return d, tea.Quit
	case "up", "k":
		d.move(-1)
	case "down", "j":
		d.move(1)
	case "home", "g":
		d.cursor = 0
	case "end", "G":
		d.cursor = max(len(d.rows)-1, 0)
	case " ":
		if len(d.rows) > 0 {
			d.rows[d.cursor].selected = !d.rows[d.cursor].selected
			d.move(1)
		}
	case "a":
		d.toggleAll()
	case "r":
		return d, tea.Batch(d.load(), d.spin.Tick)
	case "s":
		return d, push(newPicker(d.sh, d.selection()))
	case "f":
		return d, push(newRun(d.sh, fetchJob(d.selection())))
	case "p":
		return d, push(newRun(d.sh, pullJob(d.selection())))
	case "e":
		if d.sh.project.Source != d.sh.cfg.UserPath {
			return d, flash("edit it where it is defined: " + shortenMiddle(d.sh.project.Source, 60))
		}
		return d, push(newEditor(d.sh, d.sh.project))
	case "enter":
		return d, flash("repo detail is not in v1 yet — use lazygit or gitui for per-repo work")
	}
	return d, nil
}

func (d *dashboard) move(delta int) {
	if len(d.rows) == 0 {
		return
	}
	d.cursor = min(max(d.cursor+delta, 0), len(d.rows)-1)
}

// toggleAll selects everything, or clears the selection when everything is
// already selected.
func (d *dashboard) toggleAll() {
	all := true
	for _, row := range d.rows {
		if !row.selected {
			all = false
			break
		}
	}
	for i := range d.rows {
		d.rows[i].selected = !all
	}
}

// selection is the repositories an action applies to: the selected rows, or
// every repository when nothing is explicitly selected.
func (d *dashboard) selection() []model.Repo {
	var selected []model.Repo
	for _, row := range d.rows {
		if row.selected {
			selected = append(selected, row.state.Repo)
		}
	}
	if len(selected) > 0 {
		return selected
	}
	repos := make([]model.Repo, 0, len(d.rows))
	for _, row := range d.rows {
		repos = append(repos, row.state.Repo)
	}
	return repos
}

func (d *dashboard) View() string {
	t := d.sh.theme
	if len(d.rows) == 0 {
		return "\n  " + t.Muted.Render("this project has no repositories")
	}

	names := make([]string, 0, len(d.rows))
	branches := make([]string, 0, len(d.rows))
	for _, row := range d.rows {
		names = append(names, row.state.Repo.Name)
		branches = append(branches, d.branchCell(row))
	}
	// The fixed columns take 22 cells; share what is left between the two that
	// hold real names, so a long branch is cut instead of shifting the row.
	nameWidth := columnWidth(names, 6, share(d.sh.width, 1, 4, 10))
	branchWidth := columnWidth(branches, 8, share(d.sh.width, 2, 5, 12))

	var b strings.Builder
	b.WriteString("  " + t.ColumnHead.Render(
		cell("REPO", nameWidth+2)+"  "+cell("BRANCH", branchWidth)+"  "+
			cell("STATE", 7)+"  "+cell("SYNC", 9)+"  LAST COMMIT") + "\n")

	d.scroll()
	for i := d.offset; i < len(d.rows) && i < d.offset+d.visibleRows(); i++ {
		b.WriteString(d.renderRow(i, nameWidth, branchWidth) + "\n")
	}

	if d.loading > 0 {
		b.WriteString("\n  " + d.spin.View() + " " + t.Muted.Render("reading "+plural(d.loading, "repository")))
	} else if n := d.selectedCount(); n > 0 {
		b.WriteString("\n  " + t.Muted.Render(plural(n, "repository")+" selected"))
	}
	return b.String()
}

func (d *dashboard) renderRow(i, nameWidth, branchWidth int) string {
	t := d.sh.theme
	row := d.rows[i]

	cursor := "  "
	if i == d.cursor {
		cursor = t.Cursor.Render("› ")
	}
	marker := " "
	if row.selected {
		marker = t.Cursor.Render("✓")
	}

	name := cell(row.state.Repo.Name, nameWidth)
	if row.selected {
		name = t.SelectedRow.Render(name)
	}

	if !row.loaded {
		return cursor + marker + " " + name + "  " + t.Muted.Render(d.spin.View()+" reading…")
	}
	if !row.state.OK() {
		return cursor + marker + " " + name + "  " + t.Failure.Render(errLabel(row.state.Err))
	}

	st := row.state.Status
	state := t.Clean.Render(cell("clean", 7))
	if st.Dirty {
		state = t.Dirty.Render(cell("dirty", 7))
	}

	line := cursor + marker + " " + name + "  " +
		cell(d.branchCell(row), branchWidth) + "  " +
		state + "  " +
		cell(syncCell(st), 9) + "  " +
		t.Muted.Render(lastCommitCell(st))
	return truncate(line, max(d.sh.width, 1))
}

func (d *dashboard) branchCell(row dashboardRow) string {
	if !row.loaded {
		return ""
	}
	if !row.state.OK() {
		return "—"
	}
	return row.state.Status.Ref()
}

func syncCell(st git.Status) string {
	switch {
	case st.Upstream == "":
		return "—"
	case st.Ahead == 0 && st.Behind == 0:
		return "="
	default:
		return "↑" + itoa(st.Ahead) + " ↓" + itoa(st.Behind)
	}
}

func lastCommitCell(st git.Status) string {
	if st.LastCommit.IsZero() {
		return "—"
	}
	return model.Age(time.Since(st.LastCommit))
}

func errLabel(err error) string {
	switch {
	case err == nil:
		return ""
	case err == git.ErrMissing:
		return "path is missing"
	case err == git.ErrNotRepo:
		return "not a git repository"
	default:
		return err.Error()
	}
}

func (d *dashboard) selectedCount() int {
	n := 0
	for _, row := range d.rows {
		if row.selected {
			n++
		}
	}
	return n
}

// visibleRows is how many rows fit under the header line and the footer note.
func (d *dashboard) visibleRows() int {
	return max(d.sh.height-3, 1)
}

// scroll keeps the cursor inside the visible window.
func (d *dashboard) scroll() {
	visible := d.visibleRows()
	if d.cursor < d.offset {
		d.offset = d.cursor
	}
	if d.cursor >= d.offset+visible {
		d.offset = d.cursor - visible + 1
	}
	if d.offset > max(len(d.rows)-visible, 0) {
		d.offset = max(len(d.rows)-visible, 0)
	}
}
