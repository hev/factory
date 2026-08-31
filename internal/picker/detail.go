package picker

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/hev/factory/internal/ui"
)

// The detail panel: everything about the one row the cursor is on.
//
// A row is a line, and a line is about seventy cells of which the pane's own
// words want fifty. Meanwhile the child ledger has been carrying the repo, the
// plan step, the brief and the pull request all along, and the pane has been
// carrying the directory the agent is actually sitting in — none of which a
// row has ever had the width to say. The screen answered "what is running";
// the question underneath it, "what is that one doing and where", took an
// attach to answer.
//
// So the cursor drives a panel. One row stays one line and the floor still
// reads down its columns, and the row you stopped on says the rest.
const (
	// detailLabel is the width of the panel's left-hand gutter — the widest
	// label plus a space.
	detailLabel = 8
	// detailTail is how much of the agent's own transcript the panel shows
	// when there is room. Fewer than this and the reason for the last line is
	// off screen; more and the panel is the screen.
	detailTail = 5
	// detailMinTail is what is left when the terminal is too short for the
	// whole panel: one line of what the agent last said is worth more than the
	// panel disappearing entirely.
	detailMinTail = 1
	// detailChrome is the panel's fixed cost — the rule, and the meta lines
	// above the transcript.
	detailChrome = 5
)

// detailHeight is what a panel with this much tail will occupy, so the row
// window above it can be sized before either is drawn.
func detailHeight(tail int) int { return detailChrome + tail }

// detail renders the panel for one row. tail is how many lines of the agent's
// own transcript to show, and live is the most recent capture of its pane —
// fresher than the snapshot the row was built from, because the focused pane
// is re-read faster than the floor is (see picker.go).
//
// A row with nothing worth saying gets no panel rather than an empty one.
func (r Row) detail(width, tail int, live []string) []string {
	switch r.Kind {
	case KindAgent:
		return r.agentDetail(width, tail, live)
	case KindStopLine:
		return r.cordDetail(width)
	}
	return nil
}

func (r Row) agentDetail(width, tail int, live []string) []string {
	a := r.Agent
	out := []string{rule(r.Name, width, a.Health)}

	out = append(out, field("where", a.whereLine(), ui.Dim, width))
	out = append(out, field("what", a.whatLine(), ui.Dim, width))
	out = append(out, field("since", a.sinceLine(time.Now()), ui.Dim, width))

	// The verdict gets its own line only when it is one somebody should act
	// on. "ok" is the state of most of the floor most of the time and saying
	// so on every row is how a screen teaches people to stop reading it.
	if a.Health.Attention() {
		style, label := ui.Waiting, "waiting"
		if a.Health == HealthTrouble {
			style, label = ui.Trouble, "trouble"
		}
		out = append(out, field(label, a.Doing, style, width))
	}
	if link := a.linkLine(); link != "" {
		out = append(out, field("links", link, ui.Dim, width))
	}

	return append(out, transcript(live, a.Tail, tail, width)...)
}

// whereLine is the answer the row cannot give: which repo, which branch, and
// which directory on this machine.
//
// The repo is what the gaffer wrote down when it dispatched; the path is where
// the pane actually is. Showing both is the point — they agree almost always,
// and the time they do not is the bug that costs an afternoon.
func (a agentRow) whereLine() string {
	var parts []string
	if a.Repo != "" {
		parts = append(parts, a.Repo)
	}
	if a.Where.Branch != "" {
		branch := a.Where.Branch
		if a.Where.Worktree {
			branch += " (worktree)"
		}
		parts = append(parts, branch)
	}
	if a.Path != "" {
		parts = append(parts, homePath(a.Path))
	}
	if len(parts) == 0 {
		return "nowhere this can see — the pane has no directory"
	}
	return strings.Join(parts, "  ·  ")
}

// whatLine is what the gaffer sent this worker to do, which is a different
// question from what it is doing this second and is answered by a different
// source: the ledger, not the pane.
func (a agentRow) whatLine() string {
	var parts []string
	if a.Tag != "" {
		parts = append(parts, a.Tag)
	}
	if a.Issue != "" {
		parts = append(parts, issueLabel(a.Issue))
	}
	head := strings.Join(parts, " ")

	switch {
	case a.Step != "" && head != "":
		return head + "  →  " + a.Step
	case a.Step != "":
		return a.Step
	case head != "":
		return head
	case a.Gaffer:
		return "the loop — it decides what the workers above it do"
	case a.Ledger:
		return "dispatched with no plan step recorded"
	}
	return "no ledger entry — recognised by the name its gaffer gave it"
}

// sinceLine is the clock on this worker: how long it has been out, whether it
// has anything to show for it, and how long the pane has been still.
func (a agentRow) sinceLine(now time.Time) string {
	var parts []string
	if !a.DispatchedAt.IsZero() {
		parts = append(parts, "dispatched "+ui.Duration(int(now.Sub(a.DispatchedAt).Seconds()))+" ago")
	}
	switch {
	case a.PR != "":
		parts = append(parts, "PR #"+a.PR)
	case a.Ledger && !a.Gaffer:
		parts = append(parts, "no PR yet")
	}
	if a.Stale {
		parts = append(parts, "past the stale threshold")
	}
	switch {
	case a.Working:
		parts = append(parts, "moving now")
	case a.Idle > 0:
		parts = append(parts, "quiet "+ui.Duration(int(a.Idle.Seconds())))
	}
	if a.Attached {
		parts = append(parts, "you are attached")
	}
	if len(parts) == 0 {
		return "just seen — nothing timed yet"
	}
	return strings.Join(parts, "  ·  ")
}

// linkLine is the things a person would otherwise go and look up. They are
// printed whole rather than shortened, because the only use for them is to be
// copied out of the terminal.
func (a agentRow) linkLine() string {
	var parts []string
	if a.IssueURL != "" {
		parts = append(parts, a.IssueURL)
	}
	if a.Brief != "" {
		parts = append(parts, homePath(a.Brief))
	}
	return strings.Join(parts, "  ·  ")
}

// cordDetail names what pulling the cord would reach. It is the same list the
// confirm shows, put where somebody can read it before committing to the
// keystroke that asks.
func (r Row) cordDetail(width int) []string {
	out := []string{rule("stop the line", width, HealthTrouble)}
	out = append(out, field("stops", r.Detail, ui.Alarm, width))
	for _, line := range r.CordLines {
		out = append(out, "   "+ui.Dim.Render(fit(line, width-3)))
	}
	out = append(out, field("keeps", "every session that is not this factory's", ui.Dim, width))
	return out
}

// ── the parts ────────────────────────────────────────────────

// rule is the panel's heading: the session's real name, in the same shape as
// the `── sub-agents ──` separator above it, so the panel reads as part of the
// list rather than as a window on top of it.
func rule(name string, width int, health Health) string {
	head := "── " + name + " "
	style := ui.Dim
	switch health {
	case HealthTrouble:
		style = ui.Trouble
	case HealthWaiting:
		style = ui.Waiting
	}
	if fill := width - len([]rune(head)); fill > 0 {
		head += strings.Repeat("─", fill)
	}
	return style.Render(head)
}

func field(label, value string, style lipgloss.Style, width int) string {
	room := width - detailLabel - 3
	if room < 1 {
		room = 1
	}
	return "   " + ui.Dim.Render(ui.Pad(label, detailLabel)) + style.Render(fit(value, room))
}

// fit truncates to a width without padding out to it. The panel's lines are
// not a grid — padding them would fill the terminal with trailing whitespace
// that shows up the moment anybody selects the screen to copy it.
// furniture is the agent's own chrome rather than anything it has done: the
// input line echoing back what somebody typed, and the status bar under it.
//
// aboveInputBox finds the bordered box the older TUIs draw and is the right
// answer when there is one. The current ones often draw no border at all, so
// the panel — which shows several lines where the row shows one — sees the
// prompt and the token counter. This is a filter for the panel only. The
// doing column keeps the shared heuristic untouched, because a rule that
// silently swallowed a line there would be a worse failure than a noisy one.
func furniture(line string) bool {
	switch {
	case strings.HasPrefix(line, "❯"), strings.HasPrefix(line, "> "):
		return true
	case strings.HasPrefix(line, "?") && strings.Contains(line, "for shortcuts"):
		return true
	}
	for _, chrome := range []string{
		"new task?", "/clear to save", "to interrupt", "to cancel",
		"Update installed", "Restart to update", "for shortcuts",
		"How is Claude doing this session?", "0: Dismiss",
	} {
		if strings.Contains(line, chrome) {
			return true
		}
	}
	return false
}

func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) > width {
		return runewidth.Truncate(s, width, "…")
	}
	return s
}

// transcript is the agent in its own words: the last few lines it wrote,
// above the input box it parks at the bottom of its pane.
//
// live is preferred over the snapshot's copy whenever there is one, which is
// what makes the panel move at the pace of the pane rather than at the pace of
// the floor refresh.
func transcript(live, cached []string, want, width int) []string {
	lines := live
	if len(lines) == 0 {
		lines = cached
	}
	if want < 1 {
		return nil
	}

	body := trimTrailing(aboveInputBox(lines))
	var kept []string
	for i := len(body) - 1; i >= 0 && len(kept) < want; i-- {
		text := clean(body[i])
		if text == "" || furniture(text) {
			continue
		}
		kept = append([]string{text}, kept...)
	}
	if len(kept) == 0 {
		return []string{"   " + ui.Dim.Render("nothing in the pane yet")}
	}

	out := make([]string, 0, len(kept))
	for _, line := range kept {
		out = append(out, "   "+ui.Dim.Render(fit(line, width-3)))
	}
	return out
}
