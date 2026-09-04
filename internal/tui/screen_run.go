package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rustbohr/repohop/internal/model"
	"github.com/rustbohr/repohop/internal/ops"
)

// jobKind is which operation the run screen is executing.
type jobKind int

const (
	jobSwitch jobKind = iota
	jobFetch
	jobPull
)

// job is what the run screen was asked to do.
type job struct {
	kind   jobKind
	repos  []model.Repo
	branch model.BranchInfo
	// pull is this run's choice, seeded from the configured default and
	// changeable in the picker.
	pull bool
}

func switchJob(repos []model.Repo, branch model.BranchInfo, pull bool) job {
	return job{kind: jobSwitch, repos: repos, branch: branch, pull: pull}
}
func fetchJob(repos []model.Repo) job { return job{kind: jobFetch, repos: repos} }
func pullJob(repos []model.Repo) job  { return job{kind: jobPull, repos: repos} }

func (j job) verb() string {
	switch j.kind {
	case jobFetch:
		return "fetch"
	case jobPull:
		return "pull"
	default:
		return "switch to " + j.branch.Name
	}
}

// phase is where the run screen is in its lifecycle.
type phase int

const (
	phasePreflight phase = iota
	phaseChoice
	phaseRunning
	phaseDone
)

// run is the preflight / execution / summary screen.
type run struct {
	sh  *shared
	job job

	phase   phase
	spin    spinner.Model
	pre     ops.Preflight
	choice  int
	dirty   []model.RepoState
	rows    []runRow
	current int
	expand  map[int]bool
	cursor  int
	stashes int
}

// runRow is one repository's line, filled in as the operation progresses.
type runRow struct {
	repo   model.Repo
	done   bool
	sw     ops.SwitchResult
	op     ops.OpResult
	failed bool
}

// Messages for the streamed execution.
type (
	preflightMsg struct{ pre ops.Preflight }
	runEventMsg  struct {
		event runEvent
		ch    <-chan runEvent
	}
	runFinishedMsg struct{}
	stashPoppedMsg struct {
		restored int
		failed   int
	}
)

// runEvent is one repository's completion, whatever the job kind.
type runEvent struct {
	sw ops.SwitchResult
	op ops.OpResult
}

func newRun(sh *shared, j job) *run {
	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	spin.Style = sh.theme.Spinner

	r := &run{sh: sh, job: j, spin: spin, expand: map[int]bool{}}
	r.rows = make([]runRow, 0, len(j.repos))
	for _, repo := range j.repos {
		r.rows = append(r.rows, runRow{repo: repo})
	}
	return r
}

func (r *run) Init() tea.Cmd {
	if r.job.kind != jobSwitch {
		r.phase = phaseRunning
		return tea.Batch(r.spin.Tick, r.start(ops.DirtySkip))
	}
	r.phase = phasePreflight
	return tea.Batch(r.spin.Tick, r.preflight())
}

// preflight re-checks the working trees before anything is written.
func (r *run) preflight() tea.Cmd {
	return func() tea.Msg {
		return preflightMsg{pre: r.sh.runner.Preflight(r.sh.ctx, r.job.repos, r.job.branch.Name)}
	}
}

func (r *run) Title() string { return r.job.verb() }

func (r *run) Hints() []Hint {
	switch r.phase {
	case phaseChoice:
		return []Hint{{"↑/↓", "choose"}, {"enter", "confirm"}, {"esc", "cancel"}}
	case phaseDone:
		hints := []Hint{{"enter", "show the failing command"}, {"esc", "back"}}
		if r.stashes > 0 {
			hints = append(hints, Hint{"u", "restore stashes"})
		}
		return hints
	default:
		return []Hint{{"esc", "cancel"}}
	}
}

func (r *run) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case preflightMsg:
		r.pre = msg.pre
		r.dirty = msg.pre.Dirty()
		if len(r.dirty) > 0 {
			r.phase = phaseChoice
			return r, nil
		}
		r.phase = phaseRunning
		return r, r.start(ops.DirtySkip)

	case runEventMsg:
		r.record(msg.event)
		return r, waitForRun(msg.ch)

	case runFinishedMsg:
		r.phase = phaseDone
		for _, row := range r.rows {
			if row.sw.StashRef != "" {
				r.stashes++
			}
		}
		return r, nil

	case stashPoppedMsg:
		r.stashes = 0
		if msg.failed > 0 {
			return r, flash(plural(msg.restored, "stash") + " restored, " + itoa(msg.failed) + " could not be")
		}
		return r, flash(plural(msg.restored, "stash") + " restored")

	case spinner.TickMsg:
		if r.phase == phaseDone {
			return r, nil
		}
		var cmd tea.Cmd
		r.spin, cmd = r.spin.Update(msg)
		return r, cmd

	case tea.KeyMsg:
		return r.key(msg)
	}
	return r, nil
}

func (r *run) key(msg tea.KeyMsg) (screen, tea.Cmd) {
	switch r.phase {
	case phaseChoice:
		switch msg.String() {
		case "up", "k":
			r.choice = max(r.choice-1, 0)
		case "down", "j":
			r.choice = min(r.choice+1, 2)
		case "enter":
			switch r.choice {
			case 0:
				r.phase = phaseRunning
				return r, tea.Batch(r.spin.Tick, r.start(ops.DirtySkip))
			case 1:
				r.phase = phaseRunning
				return r, tea.Batch(r.spin.Tick, r.start(ops.DirtyStash))
			default:
				return r, pop
			}
		}

	case phaseDone:
		switch msg.String() {
		case "up", "k":
			r.cursor = max(r.cursor-1, 0)
		case "down", "j":
			r.cursor = min(r.cursor+1, max(len(r.rows)-1, 0))
		case "enter":
			r.expand[r.cursor] = !r.expand[r.cursor]
		case "u":
			if r.stashes > 0 {
				return r, r.popStashes()
			}
		}
	}
	return r, nil
}

// start runs the operation in the background, streaming one event per
// repository back into the message loop.
func (r *run) start(dirty ops.DirtyPolicy) tea.Cmd {
	ch := make(chan runEvent, len(r.job.repos))
	go func() {
		defer close(ch)
		switch r.job.kind {
		case jobFetch:
			r.sh.runner.Fetch(r.sh.ctx, r.job.repos, func(result ops.OpResult) {
				ch <- runEvent{op: result}
			})
		case jobPull:
			r.sh.runner.Pull(r.sh.ctx, r.job.repos, func(result ops.OpResult) {
				ch <- runEvent{op: result}
			})
		default:
			opts := ops.SwitchOptions{
				Branch: r.job.branch.Name,
				Pull:   r.job.pull,
				Dirty:  dirty,
			}
			r.sh.runner.Switch(r.sh.ctx, r.job.repos, opts, func(result ops.SwitchResult) {
				ch <- runEvent{sw: result}
			})
		}
	}()
	return waitForRun(ch)
}

func waitForRun(ch <-chan runEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return runFinishedMsg{}
		}
		return runEventMsg{event: event, ch: ch}
	}
}

// record places an event on its repository's row.
func (r *run) record(event runEvent) {
	repo := event.op.Repo
	if r.job.kind == jobSwitch {
		repo = event.sw.Repo
	}
	for i := range r.rows {
		if r.rows[i].repo.Path != repo.Path {
			continue
		}
		r.rows[i].done = true
		r.rows[i].sw = event.sw
		r.rows[i].op = event.op
		r.rows[i].failed = event.op.Err != nil || (r.job.kind == jobSwitch && !event.sw.OK())
		r.current++
		return
	}
}

// popStashes restores every stash the switch took.
func (r *run) popStashes() tea.Cmd {
	rows := r.rows
	sh := r.sh
	return func() tea.Msg {
		var restored, failed int
		for _, row := range rows {
			if row.sw.StashRef == "" {
				continue
			}
			if err := sh.runner.Git.StashPop(sh.ctx, row.repo.Path, row.sw.StashRef); err != nil {
				failed++
				continue
			}
			restored++
		}
		return stashPoppedMsg{restored: restored, failed: failed}
	}
}

func (r *run) View() string {
	switch r.phase {
	case phasePreflight:
		return "\n  " + r.spin.View() + " " + r.sh.theme.Muted.Render("checking working trees")
	case phaseChoice:
		return r.viewChoice()
	case phaseDone:
		return r.viewSummary()
	default:
		return r.viewProgress()
	}
}

// viewChoice offers a way through dirty working trees instead of a hard abort.
func (r *run) viewChoice() string {
	t := r.sh.theme
	var b strings.Builder
	have := "has"
	if len(r.dirty) != 1 {
		have = "have"
	}
	b.WriteString("\n  " + t.Warning.Render(plural(len(r.dirty), "repository")+" "+have+" local changes:") + "\n\n")
	for _, state := range r.dirty {
		b.WriteString("    " + state.Repo.Name + "  " + t.Muted.Render("on "+state.Status.Ref()) + "\n")
	}
	b.WriteString("\n")

	options := []string{
		"skip them and switch the rest",
		"stash the changes and switch (restorable afterwards)",
		"cancel",
	}
	for i, option := range options {
		cursor := "  "
		style := t.Row
		if i == r.choice {
			cursor = t.Cursor.Render("› ")
			style = t.SelectedRow
		}
		b.WriteString("  " + cursor + style.Render(option) + "\n")
	}
	return b.String()
}

func (r *run) viewProgress() string {
	t := r.sh.theme
	var b strings.Builder
	b.WriteString("\n")

	names := make([]string, 0, len(r.rows))
	for _, row := range r.rows {
		names = append(names, row.repo.Name)
	}
	nameWidth := columnWidth(names, 6, share(r.sh.width, 1, 3, 12))

	for _, row := range r.rows {
		switch {
		case !row.done:
			b.WriteString("  " + t.Muted.Render(cell(row.repo.Name, nameWidth)+"  waiting") + "\n")
		case row.failed:
			b.WriteString("  " + cell(row.repo.Name, nameWidth) + "  " + t.Failure.Render(r.rowNote(row)) + "\n")
		default:
			b.WriteString("  " + cell(row.repo.Name, nameWidth) + "  " + t.Success.Render(r.rowNote(row)) + "\n")
		}
	}
	b.WriteString("\n  " + r.spin.View() + " " + t.Muted.Render(itoa(r.current)+"/"+itoa(len(r.rows))+" done"))
	return b.String()
}

// viewSummary reproduces the transition table, the most useful output the tool
// has, with failures expandable to the exact git command.
func (r *run) viewSummary() string {
	if r.job.kind != jobSwitch {
		return r.viewOpSummary()
	}
	t := r.sh.theme

	names := make([]string, 0, len(r.rows))
	olds := make([]string, 0, len(r.rows))
	for _, row := range r.rows {
		names = append(names, row.repo.Name)
		olds = append(olds, row.sw.OldBranch)
	}
	// Bound the columns against the terminal, not against a constant: a real
	// branch name is routinely longer than any number picked in advance.
	nameWidth := columnWidth(names, 6, share(r.sh.width, 1, 4, 12))
	oldWidth := columnWidth(olds, 8, share(r.sh.width, 1, 3, 16))

	var b strings.Builder
	b.WriteString("  " + t.ColumnHead.Render(cell("REPO", nameWidth)+"  "+cell("OLD BRANCH", oldWidth+3)+"NEW BRANCH") + "\n")
	for i, row := range r.rows {
		cursor := "  "
		if i == r.cursor {
			cursor = t.Cursor.Render("› ")
		}
		line := cursor + cell(row.repo.Name, nameWidth) + "  " +
			cell(row.sw.OldBranch, oldWidth) + t.Muted.Render(" → ") + row.sw.NewBranch
		if note := r.rowNote(row); note != "" {
			style := t.Muted
			if row.failed {
				style = t.Warning
			}
			line += "  " + style.Render("("+note+")")
		}
		b.WriteString(truncate(line, max(r.sh.width, 1)) + "\n")
		b.WriteString(r.detail(i, row))
	}

	failed := 0
	for _, row := range r.rows {
		if row.failed {
			failed++
		}
	}
	b.WriteString("\n  ")
	if failed == 0 {
		b.WriteString(t.Success.Render("every repository is on " + r.job.branch.Name))
	} else {
		b.WriteString(t.Warning.Render(itoa(len(r.rows)-failed) + "/" + itoa(len(r.rows)) + " on " + r.job.branch.Name))
	}
	if r.stashes > 0 {
		b.WriteString(t.Muted.Render("  ·  " + plural(r.stashes, "stash") + " taken, press u to restore"))
	}
	return b.String()
}

func (r *run) viewOpSummary() string {
	t := r.sh.theme
	names := make([]string, 0, len(r.rows))
	for _, row := range r.rows {
		names = append(names, row.repo.Name)
	}
	nameWidth := columnWidth(names, 6, share(r.sh.width, 1, 3, 12))

	var b strings.Builder
	b.WriteString("\n")
	for i, row := range r.rows {
		cursor := "  "
		if i == r.cursor {
			cursor = t.Cursor.Render("› ")
		}
		style := t.Success
		if row.failed {
			style = t.Failure
		}
		b.WriteString(cursor + cell(row.repo.Name, nameWidth) + "  " + style.Render(r.rowNote(row)) + "\n")
		b.WriteString(r.detail(i, row))
	}
	return b.String()
}

// detail renders the expanded failure: the exact command and its stderr, so
// the user can run it themselves.
func (r *run) detail(i int, row runRow) string {
	if !r.expand[i] {
		return ""
	}
	err := row.op.Err
	if r.job.kind == jobSwitch {
		err = row.sw.Err
	}
	if err == nil {
		return ""
	}
	t := r.sh.theme
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(err.Error()), "\n") {
		b.WriteString("      " + t.Muted.Render(truncate(line, max(r.sh.width-6, 1))) + "\n")
	}
	return b.String()
}

// rowNote is the parenthesised explanation for one row.
func (r *run) rowNote(row runRow) string {
	if !row.done {
		return ""
	}
	if r.job.kind != jobSwitch {
		switch {
		case row.op.Err != nil:
			return "failed: " + row.op.Err.Error()
		case row.op.Note != "":
			return row.op.Note
		case r.job.kind == jobFetch:
			return "fetched"
		default:
			return "pulled"
		}
	}

	switch row.sw.Outcome {
	case ops.OutcomeSwitched, ops.OutcomeUnchanged:
		return row.sw.Note
	case ops.OutcomeFailed:
		return "failed: " + firstLine(row.sw.Err)
	default:
		note := row.sw.Outcome.String()
		if row.sw.Note != "" {
			note += ", " + row.sw.Note
		}
		return note
	}
}

func firstLine(err error) string {
	if err == nil {
		return ""
	}
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}
