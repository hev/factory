package picker

import (
	"strings"
	"testing"
	"time"
)

func worker() Row {
	return Row{Kind: KindAgent, Name: "worker-acme-index-backfill", Agent: agentRow{
		Instance: "acme", Tag: "~index", Harness: "codex", Working: true,
		Repo: "acme/api", Path: "/tmp/acme/api-index",
		Where:        Where{Branch: "index-rebuild", Worktree: true},
		Step:         "wire turbopuffer credential preflight",
		DispatchedAt: time.Now().Add(-5 * time.Hour), Ledger: true,
		Tail: []string{"⏺ Bash(npm test -- --watch)", "⎿  12 passed, 1 failing"},
	}}
}

func panel(r Row, tail int, live []string) string {
	return strings.Join(r.detail(120, tail, live), "\n")
}

// The panel exists to say the things a row has never had the width for. If it
// stops saying one of them, it has stopped earning its lines.
func TestPanelSaysWhereAndWhat(t *testing.T) {
	out := panel(worker(), 3, nil)
	for _, want := range []string{
		"acme/api",                              // the repo, from the ledger
		"index-rebuild",                         // the branch, from .git
		"worktree",                              // and that it is a linked one
		"api-index",                             // the directory the pane is in
		"wire turbopuffer credential preflight", // the step it was sent to do
		"dispatched 5h ago",
		"no PR yet",
		"⏺ Bash(npm test -- --watch)", // and the agent's own words
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the panel never says %q:\n%s", want, out)
		}
	}
}

// A verdict of "ok" is the state of most of the floor most of the time. Saying
// it on every row is how a screen teaches people to stop reading it.
func TestPanelOnlyCallsOutAVerdictWorthActingOn(t *testing.T) {
	row := worker()
	row.Agent.Health, row.Agent.Doing = HealthOK, "running the backfill"
	if out := panel(row, 2, nil); strings.Contains(out, "ok ") {
		t.Errorf("a healthy worker should get no verdict line:\n%s", out)
	}

	row.Agent.Health, row.Agent.Doing = HealthTrouble, "npm test failing the same way three times"
	out := panel(row, 2, nil)
	if !strings.Contains(out, "trouble") || !strings.Contains(out, "failing the same way") {
		t.Errorf("a worker in trouble should say so and say why:\n%s", out)
	}
}

// The live capture is what makes the panel stream. It wins over the snapshot's
// copy, which is up to a full refresh older.
func TestPanelPrefersTheLiveCapture(t *testing.T) {
	live := []string{"⏺ Write(src/index/backfill.ts)"}
	out := panel(worker(), 3, live)
	if !strings.Contains(out, "backfill.ts") {
		t.Errorf("the live capture should be what is shown:\n%s", out)
	}
	if strings.Contains(out, "npm test") {
		t.Errorf("the stale snapshot tail should not be shown alongside it:\n%s", out)
	}
}

// A worker with no ledger file is the documented fallback, not an error. The
// panel has to say what it does not know rather than render blanks.
func TestPanelExplainsAWorkerWithNoLedgerEntry(t *testing.T) {
	row := Row{Kind: KindAgent, Name: "worker-acme-loose", Agent: agentRow{Instance: "acme"}}
	out := panel(row, 2, nil)
	if !strings.Contains(out, "no ledger entry") {
		t.Errorf("a worker known only by its name should say so:\n%s", out)
	}
	if !strings.Contains(out, "nowhere this can see") {
		t.Errorf("a pane with no directory should say so rather than render a blank:\n%s", out)
	}
}

// The panel gives up its transcript before it gives up existing, and gives up
// existing before it pushes the list off the screen.
func TestPanelShrinksRatherThanOverflowing(t *testing.T) {
	row := worker()
	full := len(row.detail(120, detailTail, nil))
	small := len(row.detail(120, detailMinTail, nil))
	if small >= full {
		t.Errorf("a smaller tail should be fewer lines: %d vs %d", small, full)
	}

	m := &model{height: 14, width: 122, detail: true}
	m.shot = snapshot{rows: []Row{row}}
	m.refilter()
	lines := m.panelLines(120)
	if got := m.height - chrome - len(lines); got < 3 {
		t.Errorf("the panel took %d lines and left the list %d rows", len(lines), got)
	}

	// A terminal too short for even the smallest panel gets the list instead.
	m.height = 9
	if lines := m.panelLines(120); len(lines) != 0 {
		t.Errorf("a 9-line terminal should have no panel, got %d lines", len(lines))
	}
}

// The one failure this mechanism could produce is showing one agent's pane
// under another agent's name.
func TestLiveCaptureIsDiscardedWhenTheCursorHasMoved(t *testing.T) {
	focus := focusState{name: "worker-acme-index", lines: []string{"⏺ Bash(npm test)"}}
	if got := focus.linesFor("worker-acme-search"); got != nil {
		t.Errorf("another row's capture leaked through: %v", got)
	}
	if got := focus.linesFor("worker-acme-index"); len(got) != 1 {
		t.Errorf("its own row's capture was dropped: %v", got)
	}
}

// ^g ↵ is meant to be the whole gesture on a row the model has already flagged.
func TestComposeOpensWithTheModelsSentence(t *testing.T) {
	row := worker()
	row.Agent.Health, row.Agent.Doing = HealthTrouble, "npm test failing the same way three times"
	if got := openingLine(row); got != row.Agent.Doing {
		t.Errorf("compose opened with %q, want the trouble it is about", got)
	}

	row.Agent.Health = HealthOK
	if got := openingLine(row); got != "" {
		t.Errorf("a healthy row should open an empty line, got %q", got)
	}
}
