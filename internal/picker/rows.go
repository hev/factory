package picker

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hev/factory/pkg/factory"
	"github.com/hev/factory/internal/stopline"
	"github.com/hev/factory/internal/tmuxctl"
	"github.com/hev/factory/internal/ui"
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
	KindReception Kind = iota
	KindAgent
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
	Up     bool   // reception only: is the desk actually on duty?
	Agent  agentRow
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
	Idle     time.Duration // zero when the picker has not watched it long enough to say
	Attached bool
	Working  bool
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

	// Reception first — one desk per factory, so it sits above the floor it
	// opens onto rather than sorting with everything else.
	//
	// The row is there whether or not the desk is. A front door you cannot see
	// is one you cannot use, and "the desk is down" is among the more useful
	// things this screen can say — it used to say it by showing nothing, which
	// is indistinguishable from a factory that has no desk at all. ↵ on a down
	// desk puts it back on duty and then attaches.
	desk := deskSession(instance)
	receptionUp := false
	for _, s := range sessions {
		if s.Name == desk {
			receptionUp = true
			break
		}
	}
	shot.rows = append(shot.rows, deskRow(instance, receptionUp))

	// The live floor: this factory's gaffer and the workers it dispatched, most
	// recently active first. The other factories' sub-agents are not here, and
	// neither is anything else running on this machine.
	var agents []Row
	for _, s := range sessions {
		member := scope.Classify(s.Name, now)
		if member.Kind == factory.NotFactory || member.Kind == factory.Reception {
			continue // reception is already pinned above
		}
		if member.Instance != instance {
			continue
		}
		pane := panes[s.Name]
		agents = append(agents, Row{
			Kind: KindAgent, Name: s.Name,
			Search: strings.Join([]string{s.Name, member.Instance, member.Tag, pane.Path}, " "),
			Agent: agentRow{
				paneID:   pane.ID,
				Instance: member.Instance,
				Issue:    member.Issue,
				Tag:      member.Tag,
				Stale:    member.Stale,
				Harness:  procs.AgentFor(pane.PID),
				Attached: s.Attached,
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
		shot.rows = append(shot.rows, Row{Kind: KindNote, Label: emptyNote(instance, receptionUp)})
	}

	// The andon cord sits last, below everything it would stop, and only when
	// there is something running to stop. It reaches the sub-agents above it
	// and leaves reception standing, so what it says is what the screen shows.
	shot.cord = stopline.Scan(root, stopline.Factory, instance)
	if !shot.cord.Empty() {
		shot.rows = append(shot.rows, Row{Kind: KindSeparator, Label: ""})
		shot.rows = append(shot.rows, Row{
			Kind:   KindStopLine,
			Label:  "🚨 stop the line",
			Detail: shot.cord.Summary(),
			Search: "stop the line andon",
		})
	}
	return shot
}

// readPanes fills in what each sub-agent is doing and whether it is moving,
// one tmux capture per row. The captures run together because they are
// independent, and a refresh that waits for twenty of them in a row is a
// refresh you can feel.
func readPanes(rows []Row, prev, next map[string]paneState, now time.Time) {
	captured := make([][]string, len(rows))
	var wg sync.WaitGroup
	for i := range rows {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			captured[i] = tmuxctl.CapturePane(rows[i].Agent.paneID, captureLines)
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
		if label := summaries.label(rows[i].Name, digest, lines, rows[i].Agent.Working); label != "" {
			rows[i].Agent.Doing = label
		}
		if !state.changed.IsZero() {
			rows[i].Agent.Idle = now.Sub(state.changed)
		}
	}
}

// deskSession names the front desk of one factory: factory-<instance>, or the
// bootstrap desk on a machine that has not been through `factory init` and so
// has no instance to name one after.
func deskSession(instance string) string {
	if instance == "" {
		return factory.ReceptionSession
	}
	return factory.ReceptionFor(instance)
}

// deskRow is the reception line, in either of its two states. A desk that is
// down still gets a row, and the row says what to do about it.
func deskRow(instance string, up bool) Row {
	row := Row{
		Kind:   KindReception,
		Name:   deskSession(instance),
		Up:     up,
		Label:  "💁 reception",
		Detail: "the front desk — ask anything",
		Search: "reception front desk " + instance,
	}
	if !up {
		row.Detail = "off duty — ↵ puts the desk back on"
	}
	return row
}

// emptyNote explains a bare floor, which has three different causes worth
// telling apart: no factory is configured yet, this one is configured but its
// gaffer is not running, or it is running and has dispatched nothing.
func emptyNote(instance string, receptionUp bool) string {
	if instance == "" {
		return "no factory configured — run ./factory init"
	}
	if receptionUp {
		return "nothing dispatched for " + instance + " — run ./factory"
	}
	return instance + " is not running here — run ./factory to put its desk on duty"
}

// The columns every sub-agent row is laid out in. Fixed widths by hand, so the
// eye can travel down a column even when half the rows leave it blank.
const (
	colName = 18
	// Wide enough for a Linear identifier — `HEV-1234` is 8 — since issues live
	// there now. A bare GitHub `#123` still fits with room over.
	colIssue   = 8
	colTag     = 10
	colHarness = 6
	colStatus  = 9
	// prefixWidth is every column ahead of the free-text one, separators
	// included: what "doing" has to fit inside the terminal alongside.
	prefixWidth = 2 + colName + 2 + 8 + 1 + colIssue + 1 + colTag + 2 + colHarness + 2 + colStatus + 2
	// defaultWidth is what a row assumes when nothing has told it the terminal
	// size — the `--list` dump, and the first frame before tea measures.
	defaultWidth = 120
)

// render draws one row inside a terminal width cells wide.
func (r Row) render(width int) string {
	if width <= 0 {
		width = defaultWidth
	}
	switch r.Kind {
	case KindSeparator, KindNote:
		return ui.Dim.Render(r.Label)
	case KindReception:
		if !r.Up {
			return ui.Dim.Render(ui.Pad(r.Label, 18) + r.Detail)
		}
		return ui.Pad(r.Label, 18) + ui.Dim.Render(r.Detail)
	case KindStopLine:
		return ui.Alarm.Render(ui.Pad(r.Label, 18) + r.Detail)
	case KindAgent:
		return r.renderAgent(width)
	}
	return ""
}

func (r Row) renderAgent(width int) string {
	a := r.Agent

	dot := ui.Dim.Render("○")
	status := ui.Dim.Render(ui.Pad(idleLabel(a.Idle), colStatus))
	if a.Working {
		dot = ui.Working.Render("●")
		word := "active"
		switch a.Harness {
		case "claude", "codex", "aider":
			word = "working"
		}
		status = ui.Working.Render(ui.Pad(word, colStatus))
	}

	// The stale mark rides alongside the working dot rather than replacing it:
	// a worker that has been looping for hours is still streaming output, and
	// that is exactly the case worth flagging.
	stale := " "
	if a.Stale {
		stale = ui.Alarm.Render("⚠")
	}

	name := ui.Pad(r.Name, colName)
	if a.Attached {
		name = ui.Attached.Render(name)
	} else {
		name = ui.Normal.Render(name)
	}

	instance := ui.Pad("", 8)
	if a.Instance != "" {
		instance = ui.InstanceStyle(a.Instance).Render(ui.Pad(a.Instance, 8))
	}
	issue := ui.Pad("", colIssue)
	if a.Issue != "" {
		issue = ui.Issue.Render(ui.Pad(issueLabel(a.Issue), colIssue))
	}
	tag := ui.Dim.Render(ui.Pad(a.Tag, colTag))

	harness := ui.Dim.Render(ui.Pad(a.Harness, colHarness))
	switch a.Harness {
	case "claude", "codex", "aider":
		harness = ui.Agent.Render(ui.Pad(a.Harness, colHarness))
	}

	line := fmt.Sprintf("%s%s %s  %s %s %s  %s  %s  ",
		dot, stale, name, instance, issue, tag, harness, status)

	// Whatever is left of the terminal goes to the pane. It is the column that
	// changes while you watch, so it gets the room rather than a fixed width,
	// and it is the first thing to disappear on a narrow screen.
	if room := width - prefixWidth; room > 8 && a.Doing != "" {
		line += ui.Dim.Render(ui.Pad(a.Doing, room))
	}
	return strings.TrimRight(line, " ")
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
