package picker

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func agent(name string, fill func(*agentRow)) Row {
	row := Row{Kind: KindAgent, Name: name, Agent: agentRow{Instance: "acme", Harness: "claude"}}
	if fill != nil {
		fill(&row.Agent)
	}
	return row
}

// A column nothing on screen fills is alignment nobody is using. Dropping it
// is what pays for the branch column, on exactly the factories that have no
// issues because machine work never becomes one.
func TestPlanDropsColumnsNoRowFills(t *testing.T) {
	rows := []Row{
		agent("worker-acme-index", func(a *agentRow) { a.Where = Where{Branch: "index-rebuild"} }),
		agent("worker-acme-search", func(a *agentRow) { a.Where = Where{Branch: "main"} }),
	}
	plan := planColumns(rows, 140)

	if plan.issue != 0 {
		t.Errorf("no row has an issue, so the issue column should be gone (got %d)", plan.issue)
	}
	if plan.tag != 0 {
		t.Errorf("no row has a tag, so the tag column should be gone (got %d)", plan.tag)
	}
	if plan.branch == 0 {
		t.Error("both rows have a branch, so the branch column should be on screen")
	}
}

func TestPlanKeepsAColumnOneRowFills(t *testing.T) {
	rows := []Row{
		agent("worker-acme-index", nil),
		agent("worker-acme-search", func(a *agentRow) { a.Issue = "HEV-14" }),
	}
	if plan := planColumns(rows, 140); plan.issue == 0 {
		t.Error("one row with an issue is enough to keep the column: the grid is per screen, not per row")
	}
}

// The name column grew because a truncated session name is the one column a
// person cannot reconstruct from the others.
func TestPlanWidensNameToFitTheLongestOnScreen(t *testing.T) {
	long := "worker-acme-search-index-backfill"
	plan := planColumns([]Row{agent(long, nil)}, 160)
	if plan.name < len(long) && plan.name != colNameMax {
		t.Errorf("name column is %d, too narrow for %q (%d)", plan.name, long, len(long))
	}
	if plan.name > colNameMax {
		t.Errorf("name column ran away to %d; the cap is %d", plan.name, colNameMax)
	}

	// And it does not shrink below the width the screen was designed at.
	if plan := planColumns([]Row{agent("w-1", nil)}, 160); plan.name != colNameMin {
		t.Errorf("short names should still hold the column open at %d, got %d", colNameMin, plan.name)
	}
}

// On a narrow terminal the pane's own words are worth more than the columns
// the detail panel repeats in full, so those go first.
func TestPlanGivesUpColumnsBeforeThePane(t *testing.T) {
	rows := []Row{
		agent("worker-acme-index", func(a *agentRow) {
			a.Issue, a.Tag, a.Where = "HEV-14", "~index", Where{Branch: "index-rebuild"}
		}),
	}

	wide := planColumns(rows, 160)
	if wide.branch == 0 || wide.tag == 0 || wide.issue == 0 {
		t.Fatalf("a wide terminal should hold every column: %+v", wide)
	}

	narrow := planColumns(rows, 80)
	if narrow.branch != 0 {
		t.Error("the branch column should be the first to go on a narrow screen")
	}
	if narrow.doing < doingMin {
		t.Errorf("the pane got %d cells, below the %d it is worth drawing at", narrow.doing, doingMin)
	}
}

// Whatever the plan drops, the row still has to be a row.
func TestRowRendersUnderEveryPlan(t *testing.T) {
	row := agent("worker-acme-index", func(a *agentRow) {
		a.Issue, a.Tag, a.Doing = "HEV-14", "~index", "running the backfill"
		a.Where = Where{Branch: "index-rebuild"}
	})
	for _, width := range []int{40, 60, 80, 120, 200} {
		out := row.render(width, planColumns([]Row{row}, width))
		if strings.TrimSpace(out) == "" {
			t.Errorf("width %d rendered nothing", width)
		}
		if strings.Contains(out, "\n") {
			t.Errorf("width %d rendered more than one line", width)
		}
	}
}

// The branch column sizes to its content too, so `main` does not cost what
// `austria-day-scenes` costs.
func TestBranchColumnSizesToItsContent(t *testing.T) {
	short := planColumns([]Row{agent("w-1", func(a *agentRow) { a.Where = Where{Branch: "main"} })}, 160)
	if short.branch != colBranchMin {
		t.Errorf("a short branch should hold the column at %d, got %d", colBranchMin, short.branch)
	}

	long := planColumns([]Row{agent("w-1", func(a *agentRow) {
		a.Where = Where{Branch: "austria-day-scenes"}
	})}, 160)
	if long.branch != len("austria-day-scenes") {
		t.Errorf("branch column is %d, too narrow for austria-day-scenes", long.branch)
	}

	huge := planColumns([]Row{agent("w-1", func(a *agentRow) {
		a.Where = Where{Branch: "detached at 9f2c1ab-and-then-some-more"}
	})}, 200)
	if huge.branch != colBranchMax {
		t.Errorf("branch column ran to %d; the cap is %d", huge.branch, colBranchMax)
	}
}

// A regression with a lesson in it: this suite runs without a terminal, so
// lipgloss renders every style as a no-op and a row that is broken in colour
// looks perfect in a test. The attention mark was padded *after* being styled,
// ui.Pad measured its escape sequences as terminal cells, and it truncated
// itself to nothing — on screen, and only on screen.
//
// So the styled path gets exercised on purpose.
func TestTheMarkSurvivesBeingStyled(t *testing.T) {
	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })

	for _, c := range []struct {
		what  string
		row   agentRow
		glyph string
	}{
		{"trouble", agentRow{Health: HealthTrouble}, "!"},
		{"stale", agentRow{Stale: true}, "⚠"},
		{"waiting", agentRow{Health: HealthWaiting}, "?"},
	} {
		row := Row{Kind: KindAgent, Name: "worker-acme-index", Agent: c.row}
		row.Agent.Instance, row.Agent.Doing = "acme", "doing something"
		out := row.render(120, planColumns([]Row{row}, 120))
		if !strings.Contains(out, c.glyph) {
			t.Errorf("%s: the %q mark is not in the styled row:\n%q", c.what, c.glyph, out)
		}
	}
}
