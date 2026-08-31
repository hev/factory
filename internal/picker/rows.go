package picker

import (
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/hev/factory/internal/stopline"
	"github.com/hev/factory/internal/tmuxctl"
	"github.com/hev/factory/internal/ui"
	"github.com/hev/factory/pkg/factory"
)

// paneState is what one sub-agent's pane looked like on the previous refresh.
// tmux's own session_activity only advances while a client is attached, and
// almost nothing the factory runs is attached, so "is this thing moving?" has
// to be answered by watching the pane itself: same bytes as last time means
// quiet, different bytes means working.
type paneState struct {
	digest  string
	changed time.Time
}

// Kind is what one row of the picker is.
type Kind int

const (
	KindAgent Kind = iota
	KindStopLine
	KindSeparator
	KindNote
)

// Row is one line on the screen. Everything the renderer needs is decided when
// the snapshot is taken, so drawing is pure string work.
type Row struct {
	Kind   Kind
	Name   string // session name
	Search string // what the filter matches against
	Label  string // fixed-row headline
	Detail string // fixed-row explanation
	Agent  agentRow

	// CordLines is what pulling the andon cord would reach, on the row that
	// would pull it. The confirm has always spelled this out; the panel says
	// it before somebody commits to the keystroke that asks.
	CordLines []string
}

// selectable rows are the ones the cursor may land on.
func (r Row) selectable() bool { return r.Kind != KindSeparator && r.Kind != KindNote }

type agentRow struct {
	paneID   string
	Instance string
	Issue    string
	Tag      string
	Stale    bool
	Harness  string
	Doing    string
	Health   Health
	// Labelled says the model wrote Doing, rather than the heuristic reading
	// the pane's last line. The live focus read refreshes the second kind and
	// leaves the first alone; the two describe different things, and swapping
	// between them every third of a second reads as a glitch.
	Labelled bool
	Idle     time.Duration // zero when the picker has not watched it long enough to say
	Attached bool
	Working  bool
	Gaffer   bool // the loop rather than one of the workers it dispatched

	// Where the work is happening. Path and Where come from the pane, which is
	// the only one of these that is true by observation rather than by report;
	// Repo comes from the ledger, and the two are shown together because a
	// worker in the wrong worktree looks right from every other column.
	Path  string
	Repo  string
	Where Where

	// What it was sent to do, from the ledger. None of this fits on a row —
	// it is the detail panel's whole reason for existing.
	Step         string
	Brief        string
	IssueURL     string
	PR           string
	DispatchedAt time.Time
	Ledger       bool

	// Tail is the last of the pane, kept so the panel can show the agent's own
	// words rather than one summarised line. It is what was captured on the
	// refresh that built this row, and the focused row's is replaced faster
	// than that (see picker.go).
	Tail []string
}

// snapshot is one reading of the floor: the rows to draw, what the andon cord
// would reach if it were pulled right now, and what the panes looked like, so
// the next reading can tell which of them moved.
type snapshot struct {
	rows  []Row
	cord  stopline.Report
	panes map[string]paneState
}

// collect reads tmux, the configured instances, the child ledger and the
// process table, and turns them into the rows for one factory. Every call is a
// fresh read, so a sub-agent dispatched while the picker is open shows up on
// the next refresh.
//
// An empty instance means the machine has no factory configured yet, where the
// only thing to show is the bootstrap desk.
//
// prev is the previous reading's panes, which is how a row knows whether its
// sub-agent is working. The first reading of a session has no previous, so it
// falls back to whatever the agent itself says on screen.
func collect(root, instance string, prev map[string]paneState) snapshot {
	now := time.Now()
	scope := factory.NewScope(root)
	procs := factory.Snapshot()
	sessions := tmuxctl.ListSessions()
	panes := tmuxctl.ActivePanes()

	shot := snapshot{panes: map[string]paneState{}}

	// The live floor: this factory's gaffer and the workers it dispatched, most
	// recently active first. The other factories' sub-agents are not here, and
	// neither is anything else running on this machine.
	var agents []Row
	for _, s := range sessions {
		member := scope.Classify(s.Name, now)
		if member.Kind == factory.NotFactory {
			continue
		}
		if member.Instance != instance {
			continue
		}
		pane := panes[s.Name]
		agents = append(agents, Row{
			Kind: KindAgent, Name: s.Name,
			Search: strings.Join([]string{
				s.Name, member.Instance, member.Tag, member.Repo, member.Step, pane.Path,
			}, " "),
			Agent: agentRow{
				paneID:   pane.ID,
				Instance: member.Instance,
				Issue:    member.Issue,
				Tag:      member.Tag,
				Stale:    member.Stale,
				Harness:  procs.AgentFor(pane.PID),
				Attached: s.Attached,
				Gaffer:   member.Kind == factory.Gaffer,

				Path: pane.Path,
				Repo: member.Repo,

				Step:         member.Step,
				Brief:        member.Brief,
				IssueURL:     member.IssueURL,
				PR:           member.PR,
				DispatchedAt: member.DispatchedAt,
				Ledger:       member.Ledger,
			},
		})
	}
	readPanes(agents, prev, shot.panes, now)

	// Labels are cached per session, machine-wide. Sessions that are gone take
	// their cache entry with them; the other factories' are alive and stay.
	live := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		live[s.Name] = true
	}
	summaries.forget(live)

	shot.rows = append(shot.rows, Row{Kind: KindSeparator, Label: "── sub-agents ──"})
	if len(agents) > 0 {
		shot.rows = append(shot.rows, agents...)
	} else {
		shot.rows = append(shot.rows, Row{Kind: KindNote, Label: emptyNote(instance)})
	}

	// The andon cord sits last, below everything it would stop, and only when
	// there is something running to stop. It reaches the gaffers above it and
	// leaves reception and the workers standing, so what it says is what the
	// screen shows.
	shot.cord = stopline.Scan(root, instance)
	if !shot.cord.Empty() {
		shot.rows = append(shot.rows, Row{Kind: KindSeparator, Label: ""})
		shot.rows = append(shot.rows, Row{
			Kind:      KindStopLine,
			Label:     "🚨 stop the line",
			Detail:    shot.cord.Summary(),
			Search:    "stop the line andon",
			CordLines: shot.cord.Lines(),
		})
	}
	return shot
}

// readPanes fills in what each sub-agent is doing, where it is doing it, and
// whether it is moving: one tmux capture and one location read per row. Both
// run together because they are independent, and a refresh that waits for
// twenty of them in a row is a refresh you can feel.
func readPanes(rows []Row, prev, next map[string]paneState, now time.Time) {
	captured := make([][]string, len(rows))
	var wg sync.WaitGroup
	for i := range rows {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			captured[i] = tmuxctl.CapturePane(rows[i].Agent.paneID, captureLines)
		}(i)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rows[i].Agent.Where = whereOf(rows[i].Agent.Path)
		}(i)
	}
	wg.Wait()

	for i := range rows {
		lines := captured[i]
		digest := paneDigest(lines)
		was, seen := prev[rows[i].Name]

		state := paneState{digest: digest, changed: was.changed}
		switch {
		case !seen:
			// First sight. The picker has watched this pane for no time at all,
			// so it has nothing to say about how long it has been quiet.
		case digest != was.digest:
			state.changed = now
		}
		next[rows[i].Name] = state

		// The heuristic is what the pane literally says and is always there.
		// The model's label is better when it has caught up, so it wins when
		// there is one.
		rows[i].Agent.Working = running(lines) || (seen && digest != was.digest)
		rows[i].Agent.Doing = paneSummary(lines)
		rows[i].Agent.Tail = lines
		if label, health := summaries.label(rows[i].Name, digest, lines, rows[i].Agent.Working); label != "" {
			rows[i].Agent.Doing = label
			rows[i].Agent.Health = health
			rows[i].Agent.Labelled = true
		}
		if !state.changed.IsZero() {
			rows[i].Agent.Idle = now.Sub(state.changed)
		}
	}
}

// emptyNote explains a bare floor, which has three different causes worth
// telling apart: no factory is configured yet, this one is configured but its
// gaffer is not running, or it is running and has dispatched nothing.
func emptyNote(instance string) string {
	if instance == "" {
		return "no factory configured — run ./factory init"
	}
	return "nothing dispatched for " + instance + " — run ./factory"
}

// The columns a sub-agent row is laid out in. Widths are decided by hand
// rather than by a table layout, so the eye can travel down a column even when
// half the rows leave it blank — but which columns are *there* is decided per
// screenful, because a column that is empty on every visible row is nine cells
// of alignment nobody is using.
//
// That is not the same as collapsing a column per row, which would make every
// row a different shape. A column is in or out for the whole screen, and the
// screen stays a grid.
const (
	colNameMin = 18
	colNameMax = 32
	colInst    = 8
	// Wide enough for a Linear identifier — `HEV-1234` is 8 — since issues live
	// there now. A bare GitHub `#123` still fits with room over.
	colIssue = 8
	colTag   = 10
	// The branch column sizes to its content between these, the way the name
	// column does. `main` is four cells and `austria-day-scenes` is eighteen,
	// and a fixed width in between either wastes the first or truncates the
	// second. When there is no room for even the minimum the column is dropped
	// whole rather than shrunk into uselessness.
	colBranchMin = 10
	colBranchMax = 20
	colHarness   = 6
	colStatus    = 9
	// doingMin is the width the plan tries to leave the free-text column. An
	// optional column is dropped rather than squeezing the pane below it.
	doingMin = 24
	// doingFloor is the width below which the pane is not drawn at all. It is
	// under doingMin on purpose: the two are a target and a hard limit, and a
	// narrow terminal that has already given up every optional column should
	// show a short label rather than none.
	doingFloor = 12
	// defaultWidth is what a row assumes when nothing has told it the terminal
	// size — the `--list` dump, and the first frame before tea measures.
	defaultWidth = 120
)

// columns is the width plan for one screenful of rows. A zero means the column
// is not on screen at all.
type columns struct {
	name, instance, issue, tag, branch, harness, status, doing int
}

// planColumns decides the shape of the grid from the rows that are actually on
// it and the width there is to draw them in.
//
// Two things earn their keep here. The name column grows to fit the longest
// session on screen instead of truncating `worker-acme-search-index` at
// eighteen cells, and the optional columns disappear when nothing fills them —
// which on a factory doing machine work is usually the issue column, because
// machine work never becomes an issue (contracts/queues.md).
func planColumns(rows []Row, width int) columns {
	if width <= 0 {
		width = defaultWidth
	}
	plan := columns{name: colNameMin, instance: colInst, harness: colHarness, status: colStatus}

	for _, row := range rows {
		if row.Kind != KindAgent {
			continue
		}
		if n := len([]rune(row.Name)); n > plan.name {
			plan.name = n
		}
		if row.Agent.Issue != "" {
			plan.issue = colIssue
		}
		if row.Agent.Tag != "" {
			plan.tag = colTag
		}
		if n := len([]rune(row.Agent.Where.Branch)); n > plan.branch {
			plan.branch = n
		}
	}
	if plan.name > colNameMax {
		plan.name = colNameMax
	}
	if plan.branch > 0 {
		plan.branch = min(max(plan.branch, colBranchMin), colBranchMax)
	}

	// What is left over goes to the pane. When that is not enough to read, the
	// columns whose content the detail panel repeats in full are the ones that
	// go — the branch first, since it is the newest and the panel says it
	// alongside the repo and the path it belongs to.
	for _, drop := range []*int{&plan.branch, &plan.tag, &plan.issue} {
		if width-plan.prefix() >= doingMin {
			break
		}
		*drop = 0
	}

	// Last, the name gives back what it took. A truncated session name is a
	// real cost, but it is a smaller one than a screen with no room left to
	// say what any of these agents is doing.
	if width-plan.prefix() < doingMin && plan.name > colNameMin {
		plan.name = colNameMin
	}

	if plan.doing = width - plan.prefix(); plan.doing < 0 {
		plan.doing = 0
	}
	return plan
}

// prefix is every column ahead of the free-text one, separators included: what
// "doing" has to fit inside the terminal alongside. A dropped column costs
// nothing, separator and all.
func (c columns) prefix() int {
	// The mark and the dot, then a space before the name.
	total := 3 + c.name
	for _, w := range []int{c.instance, c.issue, c.tag, c.branch, c.harness, c.status} {
		if w > 0 {
			total += w + 2
		}
	}
	return total
}

// render draws one row inside a terminal width cells wide, in the given grid.
func (r Row) render(width int, plan columns) string {
	if width <= 0 {
		width = defaultWidth
	}
	switch r.Kind {
	case KindSeparator, KindNote:
		return ui.Dim.Render(r.Label)
	case KindStopLine:
		return ui.Alarm.Render(ui.Pad(r.Label, 18) + r.Detail)
	case KindAgent:
		return r.renderAgent(width, plan)
	}
	return ""
}

func (r Row) renderAgent(width int, plan columns) string {
	a := r.Agent

	dot := ui.Dim.Render("○")
	status := ui.Dim.Render(ui.Pad(idleLabel(a.Idle), plan.status))
	if a.Working {
		dot = ui.Working.Render("●")
		word := "active"
		switch a.Harness {
		case "claude", "codex", "aider":
			word = "working"
		}
		status = ui.Working.Render(ui.Pad(word, plan.status))
	}

	// An agent that has stopped to ask something is not idle in any sense a
	// person cares about, and "idle 12m" is the reading that costs the most:
	// it is the row you scroll past.
	if !a.Working && a.Health == HealthWaiting {
		status = ui.Waiting.Render(ui.Pad("waiting", plan.status))
	}

	// mark is styled and exactly one cell, so it is not padded: ui.Pad measures
	// terminal cells and an escape sequence is not one, so padding a rendered
	// string truncates it through the middle of its own colour codes.
	line := dot + mark(a) + " " + name(r, plan.name)
	line += pad(instanceCell(a, plan.instance), plan.instance)
	line += pad(issueCell(a, plan.issue), plan.issue)
	line += pad(ui.Dim.Render(ui.Pad(a.Tag, plan.tag)), plan.tag)
	line += pad(branchCell(a, plan.branch), plan.branch)
	line += pad(harnessCell(a, plan.harness), plan.harness)
	line += pad(status, plan.status)

	// Whatever is left of the terminal goes to the pane. It is the column that
	// changes while you watch, so it gets the room rather than a fixed width,
	// and it is the first thing to disappear on a narrow screen.
	if room := plan.doing; room >= doingFloor && a.Doing != "" {
		line += doingStyle(a).Render(ui.Pad(a.Doing, room))
	}
	return strings.TrimRight(line, " ")
}

// pad appends a column and the two cells that separate it from the next, or
// nothing at all when the plan dropped it.
func pad(cell string, width int) string {
	if width <= 0 {
		return ""
	}
	return "  " + cell
}

// mark is the one cell that answers "does this need me?".
//
// There is room for exactly one glyph here and three things could claim it, so
// they are ranked by what a person would do about them: rescue it, look at it,
// answer it. The detail panel carries all three, which is what makes ranking
// them on the row honest rather than lossy.
func mark(a agentRow) string {
	switch {
	case a.Health == HealthTrouble:
		return ui.Alarm.Render("!")
	case a.Stale:
		return ui.Alarm.Render("⚠")
	case a.Health == HealthWaiting:
		return ui.Waiting.Render("?")
	}
	return " "
}

// doingStyle colours the pane's own words by the verdict on them. A label that
// says something is wrong is worth nothing if it is drawn the same grey as the
// twenty rows that are fine.
func doingStyle(a agentRow) lipgloss.Style {
	switch a.Health {
	case HealthTrouble:
		return ui.Trouble
	case HealthWaiting:
		return ui.Waiting
	}
	return ui.Dim
}

func name(r Row, width int) string {
	padded := ui.Pad(r.Name, width)
	if r.Agent.Attached {
		return ui.Attached.Render(padded)
	}
	return ui.Normal.Render(padded)
}

func instanceCell(a agentRow, width int) string {
	if a.Instance == "" {
		return ui.Pad("", width)
	}
	return ui.InstanceStyle(a.Instance).Render(ui.Pad(a.Instance, width))
}

func issueCell(a agentRow, width int) string {
	if a.Issue == "" {
		return ui.Pad("", width)
	}
	return ui.Issue.Render(ui.Pad(issueLabel(a.Issue), width))
}

// branchCell is the answer to "where is this happening" that fits on a row.
// The repo is in the panel next to it: workers of one factory are usually all
// in the same repo, and the branch is what tells them apart.
func branchCell(a agentRow, width int) string {
	return ui.Branch.Render(ui.Pad(a.Where.Branch, width))
}

func harnessCell(a agentRow, width int) string {
	switch a.Harness {
	case "claude", "codex", "aider":
		return ui.Agent.Render(ui.Pad(a.Harness, width))
	}
	return ui.Dim.Render(ui.Pad(a.Harness, width))
}

// idleLabel is how long a sub-agent has been quiet, counted from the last time
// its pane changed under this picker. A zero duration means the picker has not
// been open long enough to know, and under a minute is not worth a number:
// both read as plain "idle" rather than as a figure nobody should trust.
func idleLabel(idle time.Duration) string {
	if idle < time.Minute || idle > 365*24*time.Hour {
		return "idle"
	}
	return "idle " + ui.Duration(int(idle.Seconds()))
}

// issueLabel decorates an issue identifier the way its tracker writes it: a
// Linear one (`HEV-14`) already reads as an identifier and is left alone, while
// a bare number is the GitHub form and gets the `#` that makes it one.
func issueLabel(id string) string {
	for _, r := range id {
		if r < '0' || r > '9' {
			return id
		}
	}
	return "#" + id
}
