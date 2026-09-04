package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rustbohr/repohop/internal/git"
	"github.com/rustbohr/repohop/internal/model"
	"github.com/rustbohr/repohop/internal/ops"
	"github.com/sahilm/fuzzy"
)

// picker is the fuzzy branch picker: the screen that replaces fzf, and the one
// that most needs to feel responsive.
type picker struct {
	sh    *shared
	repos []model.Repo

	all      []model.BranchInfo
	matches  []pickerMatch
	query    string
	cursor   int
	offset   int
	loading  bool
	failures []ops.OpResult
	spin     spinner.Model

	// pull is this switch's choice, seeded from the configured default.
	pull bool
	// fetching is true while remote refs are being updated in the background.
	fetching bool
	// fetchedOnce records that the automatic fetch has already run.
	fetchedOnce bool
	// fetchNote reports a fetch that did not go cleanly.
	fetchNote string
}

// pickerMatch is one candidate plus the positions the query matched, so the
// list can highlight them.
type pickerMatch struct {
	info      model.BranchInfo
	positions []int
}

// fetchedMsg reports that the background fetch has finished.
type fetchedMsg struct{ failures int }

// branchesLoadedMsg carries the enumerated branches back into the model.
type branchesLoadedMsg struct {
	sets     map[string]git.BranchSet
	failures []ops.OpResult
}

func newPicker(sh *shared, repos []model.Repo) *picker {
	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	spin.Style = sh.theme.Spinner
	return &picker{
		sh:      sh,
		repos:   repos,
		loading: true,
		spin:    spin,
		pull:    sh.cfg.Settings.Pull,
	}
}

func (p *picker) Init() tea.Cmd {
	// The local refs are on disk, so the list appears at once; the fetch runs
	// alongside it and the list is rebuilt when it lands. Waiting on the
	// network before showing anything is what made the old script's
	// --no-fetch worth having.
	cmds := []tea.Cmd{p.spin.Tick, p.loadBranches()}
	if p.sh.cfg.Settings.Fetch {
		p.fetching, p.fetchedOnce = true, true
		cmds = append(cmds, p.startFetch())
	}
	return tea.Batch(cmds...)
}

func (p *picker) loadBranches() tea.Cmd {
	return func() tea.Msg {
		sets, failures := p.sh.runner.Branches(p.sh.ctx, p.repos)
		return branchesLoadedMsg{sets: sets, failures: failures}
	}
}

// startFetch updates the remote refs, then reloads the candidate list.
func (p *picker) startFetch() tea.Cmd {
	return func() tea.Msg {
		results := p.sh.runner.Fetch(p.sh.ctx, p.repos, nil)
		return fetchedMsg{failures: ops.Failures(results)}
	}
}

func (p *picker) Title() string { return "switch — pick a branch" }

func (p *picker) Hints() []Hint {
	return []Hint{
		{"type", "filter"},
		{"↑/↓", "move"},
		{"enter", "switch"},
		{"alt+f", "fetch again"},
		{"alt+p", "pull " + onOff(p.pull)},
		{"esc", "cancel"},
	}
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func (p *picker) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case branchesLoadedMsg:
		p.loading = false
		p.failures = msg.failures
		p.all = model.CollectBranches(p.repos, msg.sets)
		p.refilter()
		return p, nil

	case fetchedMsg:
		p.fetching = false
		p.fetchNote = ""
		if msg.failures > 0 {
			// A repository that could not be fetched is worth saying, but the
			// branches already known locally are still there to switch to.
			p.fetchNote = plural(msg.failures, "repository") + " could not be fetched"
		}
		return p, p.loadBranches()

	case spinner.TickMsg:
		if !p.loading && !p.fetching {
			return p, nil
		}
		var cmd tea.Cmd
		p.spin, cmd = p.spin.Update(msg)
		return p, cmd

	case tea.KeyMsg:
		return p.key(msg)
	}
	return p, nil
}

func (p *picker) key(msg tea.KeyMsg) (screen, tea.Cmd) {
	switch msg.String() {
	case "up", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "pgup":
		p.move(-p.visibleRows())
	case "pgdown":
		p.move(p.visibleRows())
	case "backspace":
		if p.query != "" {
			runes := []rune(p.query)
			p.query = string(runes[:len(runes)-1])
			p.filter()
		}
	case "ctrl+u":
		p.query = ""
		p.filter()
	case "alt+f":
		if !p.fetching {
			p.fetching = true
			return p, tea.Batch(p.spin.Tick, p.startFetch())
		}
	case "alt+p":
		p.pull = !p.pull
	case "enter":
		if p.cursor < len(p.matches) {
			branch := p.matches[p.cursor].info
			return p, push(newRun(p.sh, switchJob(p.repos, branch, p.pull)))
		}
	default:
		// Alt combinations are commands, never text: typing filters, but
		// alt+something that is not bound must not end up in the query.
		if msg.Alt {
			break
		}
		if msg.Type == tea.KeyRunes {
			p.query += string(msg.Runes)
			p.filter()
		} else if msg.Type == tea.KeySpace {
			p.query += " "
			p.filter()
		}
	}
	return p, nil
}

func (p *picker) move(delta int) {
	if len(p.matches) == 0 {
		return
	}
	p.cursor = min(max(p.cursor+delta, 0), len(p.matches)-1)
}

// refilter rebuilds the match list, keeping the cursor on the branch it was
// already on: a background fetch must not move the selection under the user.
func (p *picker) refilter() {
	var was string
	if p.cursor < len(p.matches) {
		was = p.matches[p.cursor].info.Name
	}
	p.filter()
	if was == "" {
		return
	}
	for i, match := range p.matches {
		if match.info.Name == was {
			p.cursor = i
			return
		}
	}
}

// filter re-ranks the candidates for the current query. Ordering: an exact
// match first, then fuzzy score, then how many repositories carry the branch
// (one that exists everywhere is more likely the one you want), then name.
func (p *picker) filter() {
	p.cursor, p.offset = 0, 0

	if p.query == "" {
		p.matches = make([]pickerMatch, 0, len(p.all))
		for _, info := range p.all {
			p.matches = append(p.matches, pickerMatch{info: info})
		}
		return
	}

	found := fuzzy.FindFrom(p.query, branchSource(p.all))
	scored := make([]struct {
		match pickerMatch
		score int
		exact bool
	}, 0, len(found))
	for _, m := range found {
		info := p.all[m.Index]
		scored = append(scored, struct {
			match pickerMatch
			score int
			exact bool
		}{
			match: pickerMatch{info: info, positions: m.MatchedIndexes},
			score: m.Score,
			exact: info.Name == p.query,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		a, b := scored[i], scored[j]
		if a.exact != b.exact {
			return a.exact
		}
		if a.score != b.score {
			return a.score > b.score
		}
		if ca, cb := a.match.info.Count(), b.match.info.Count(); ca != cb {
			return ca > cb
		}
		return a.match.info.Name < b.match.info.Name
	})

	p.matches = make([]pickerMatch, 0, len(scored))
	for _, s := range scored {
		p.matches = append(p.matches, s.match)
	}
}

// branchSource adapts the candidate list to fuzzy.Source.
type branchSource []model.BranchInfo

func (s branchSource) String(i int) string { return s[i].Name }
func (s branchSource) Len() int            { return len(s) }

func (p *picker) View() string {
	t := p.sh.theme
	if p.loading {
		return "\n  " + p.spin.View() + " " + t.Muted.Render("reading branches from "+plural(len(p.repos), "repository"))
	}

	list := p.renderList(p.listWidth())
	switch p.layout() {
	case layoutSide:
		preview := p.renderPreview(p.sh.width - p.listWidth() - 3)
		return joinColumns(list, t.Preview.Render(preview), p.sh.height)
	case layoutBelow:
		return list + "\n" + t.Muted.Render(strings.Repeat("─", max(p.sh.width-2, 1))) + "\n" + p.renderPreview(p.sh.width-2)
	default:
		return list + "\n  " + t.Muted.Render(p.summaryLine())
	}
}

// layout picks how the preview is shown at the current width.
type layout int

const (
	layoutSide layout = iota
	layoutBelow
	layoutSummary
)

func (p *picker) layout() layout {
	switch {
	case p.sh.width >= 100:
		return layoutSide
	case p.sh.width >= 60:
		return layoutBelow
	default:
		return layoutSummary
	}
}

func (p *picker) listWidth() int {
	if p.layout() == layoutSide {
		return p.sh.width * 55 / 100
	}
	return p.sh.width
}

func (p *picker) visibleRows() int {
	rows := p.sh.height - 2
	if p.layout() == layoutBelow {
		rows -= len(p.repos) + 2
	}
	return max(rows, 3)
}

func (p *picker) renderList(width int) string {
	t := p.sh.theme

	var b strings.Builder
	b.WriteString("  " + t.Key.Render("› ") + p.query + t.Cursor.Render("▏") + "\n")
	status := plural(len(p.matches), "branch") + " of " + itoa(len(p.all)) + "  ·  pull " + onOff(p.pull)
	switch {
	case p.fetching:
		status += "  ·  " + p.spin.View() + " fetching"
	case p.fetchNote != "":
		status += "  ·  " + p.fetchNote
	}
	b.WriteString("  " + t.Muted.Render(status) + "\n")

	if len(p.matches) == 0 {
		b.WriteString("\n  " + t.Muted.Render("no branch matches"))
		return b.String()
	}

	p.scroll()
	visible := p.visibleRows()
	nameWidth := max(width-10, 8)
	for i := p.offset; i < len(p.matches) && i < p.offset+visible; i++ {
		match := p.matches[i]
		cursor := "  "
		if i == p.cursor {
			cursor = t.Cursor.Render("› ")
		}
		count := t.Muted.Render(itoa(match.info.Count()) + "/" + itoa(len(p.repos)))
		name := highlight(truncate(match.info.Name, nameWidth), match.positions, t)
		b.WriteString(cursor + cell(name, nameWidth) + "  " + count + "\n")
	}
	return b.String()
}

// highlight styles the characters the query matched.
func highlight(name string, positions []int, t Theme) string {
	if len(positions) == 0 {
		return name
	}
	marked := make(map[int]bool, len(positions))
	for _, pos := range positions {
		marked[pos] = true
	}
	var b strings.Builder
	for i, r := range []rune(name) {
		if marked[i] {
			b.WriteString(t.Match.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// renderPreview shows which repositories carry the highlighted branch. The
// data is pure ref lookups computed once, so moving the cursor costs nothing.
func (p *picker) renderPreview(width int) string {
	t := p.sh.theme
	if p.cursor >= len(p.matches) {
		return ""
	}
	info := p.matches[p.cursor].info

	names := make([]string, 0, len(info.In))
	for _, presence := range info.In {
		names = append(names, presence.Repo.Name)
	}
	nameWidth := columnWidth(names, 6, max(width/3, 8))

	var b strings.Builder
	b.WriteString(t.Title.Render(truncate(info.Name, max(width, 1))) + "\n")
	b.WriteString(t.Muted.Render(p.summaryLine()) + "\n\n")
	for _, presence := range info.In {
		mark := t.Muted.Render("·")
		if presence.Any() {
			mark = t.Success.Render("✓")
		}
		line := mark + " " + cell(presence.Repo.Name, nameWidth) + "  " + t.Muted.Render(presence.Where())
		b.WriteString(truncate(line, max(width, 1)) + "\n")
	}
	for _, failure := range p.failures {
		b.WriteString(t.Failure.Render("! "+cell(failure.Repo.Name, nameWidth)+"  "+errLabel(failure.Err)) + "\n")
	}
	return b.String()
}

func (p *picker) summaryLine() string {
	if p.cursor >= len(p.matches) {
		return ""
	}
	info := p.matches[p.cursor].info
	return itoa(info.Count()) + "/" + itoa(len(p.repos)) + " repos"
}

func (p *picker) scroll() {
	visible := p.visibleRows()
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+visible {
		p.offset = p.cursor - visible + 1
	}
	if p.offset > max(len(p.matches)-visible, 0) {
		p.offset = max(len(p.matches)-visible, 0)
	}
}

// joinColumns places the preview beside the list.
func joinColumns(left, right string, height int) string {
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")
	width := 0
	for _, line := range leftLines {
		width = max(width, lipgloss.Width(line))
	}

	rows := max(len(leftLines), len(rightLines))
	rows = min(rows, max(height, 1))

	var b strings.Builder
	for i := range rows {
		var l, r string
		if i < len(leftLines) {
			l = leftLines[i]
		}
		if i < len(rightLines) {
			r = rightLines[i]
		}
		b.WriteString(pad(l, width+2) + r + "\n")
	}
	return b.String()
}
