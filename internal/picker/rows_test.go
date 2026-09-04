package picker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hev/factory/internal/tmuxctl"
	"github.com/hev/factory/pkg/factory"
)

func TestEmptyFloorNotesPointAtWhatToDo(t *testing.T) {
	if got := emptyNote("acme"); got != "nothing dispatched for acme — run ./factory" {
		t.Fatalf("empty floor note = %q", got)
	}
	// The bootstrap floor now carries the row that fixes it, so the note names
	// the row rather than a command somebody has to leave the screen to run.
	if got := emptyNote(""); got != "no factory configured — ✚ new line below sets one up" {
		t.Fatalf("bootstrap note = %q", got)
	}
}

// A desk is offered where there is somewhere to open it, and nowhere else: a
// factory whose workspace is not checked out on this machine would give a row
// that fails on ↵, which is worse than no row.
func TestReceptionRowNeedsAWorkspaceOnThisMachine(t *testing.T) {
	home := isolate(t)
	workspace := filepath.Join(home, "acme")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	here := factory.Instance{Name: "acme", WorkspacePath: workspace}
	row, ok := receptionRow(here, nil)
	if !ok {
		t.Fatal("a checked-out workspace should offer a desk")
	}
	if row.Kind != KindReception || row.Name != "reception-acme" {
		t.Fatalf("desk row = %v %q", row.Kind, row.Name)
	}
	if row.Door.Live {
		t.Error("no session exists, so the desk is not open")
	}
	if !strings.Contains(row.Detail, "acme's front desk") {
		t.Errorf("closed desk detail = %q", row.Detail)
	}

	away := factory.Instance{Name: "acme", WorkspacePath: filepath.Join(home, "not-here")}
	if _, ok := receptionRow(away, nil); ok {
		t.Error("a workspace that is not on this machine should offer no desk")
	}
	if _, ok := receptionRow(factory.Instance{Name: "acme"}, nil); ok {
		t.Error("a factory with no workspace configured should offer no desk")
	}
}

// An open desk says so, so ↵ reads as "back to the conversation" rather than
// "start another one".
func TestReceptionRowKnowsTheDeskIsOpen(t *testing.T) {
	home := isolate(t)
	workspace := filepath.Join(home, "acme")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	sessions := map[string]tmuxctl.Session{
		"reception-acme": {Name: "reception-acme", Attached: true},
	}

	row, ok := receptionRow(factory.Instance{Name: "acme", WorkspacePath: workspace}, sessions)
	if !ok {
		t.Fatal("expected a desk row")
	}
	if !row.Door.Live || !row.Door.Attached {
		t.Fatalf("desk = %+v, want live and attached", row.Door)
	}
	if !strings.Contains(row.Detail, "goes back to it") {
		t.Errorf("open desk detail = %q", row.Detail)
	}
}

// Both door rows are things the cursor can land on. A row that opens something
// and cannot be selected is furniture.
func TestDoorRowsAreSelectable(t *testing.T) {
	for _, kind := range []Kind{KindReception, KindNewLine} {
		if !(Row{Kind: kind}).selectable() {
			t.Errorf("kind %v should be selectable", kind)
		}
	}
}

// isolate points every home-relative read at a temp dir, so a section label
// built in a test reflects the files the test wrote and not this machine's.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("FACTORY_HOLDS_DIR", filepath.Join(home, "holds"))
	return home
}

func TestSectionDetailCarriesTheLsLine(t *testing.T) {
	home := isolate(t)
	now := time.Now()
	inst := factory.Instance{
		Name: "acme", PlansRepo: "hev/acme", PlansBranch: "docs",
		Runtime: "resident", HomeHost: "elsewhere",
	}

	if got, want := sectionDetail(inst, now), "hev/acme@docs · resident · no beat yet · home: elsewhere"; got != want {
		t.Fatalf("before any beat = %q, want %q", got, want)
	}

	beats := filepath.Join(home, ".factory", "beats")
	if err := os.MkdirAll(beats, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(beats, "acme.jsonl")
	if err := os.WriteFile(log, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := now.Add(-5 * time.Minute)
	if err := os.Chtimes(log, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := factory.Hold("acme", "test"); err != nil {
		t.Fatal(err)
	}

	if got, want := sectionDetail(inst, now), "hev/acme@docs · resident · beat 5m ago · held · home: elsewhere"; got != want {
		t.Fatalf("beaten and held = %q, want %q", got, want)
	}
}

func TestSectionDetailElidesDefaults(t *testing.T) {
	isolate(t)
	inst := factory.Instance{Name: "solo", PlansRepo: "hev/solo"}
	// No @main, no runtime, no held, no home — every token that would be on
	// each section is off it.
	if got, want := sectionDetail(inst, time.Now()), "hev/solo · no beat yet"; got != want {
		t.Fatalf("defaults = %q, want %q", got, want)
	}
}

func TestLineNoteTellsTheThreeCausesApart(t *testing.T) {
	isolate(t)
	away := factory.Instance{Name: "acme", HomeHost: "elsewhere"}
	if got := lineNote(away); got != "home is elsewhere — ./factory leaves it alone here" {
		t.Fatalf("away note = %q", got)
	}

	down := factory.Instance{Name: "acme", Runtime: factory.RuntimeResident}
	if got := lineNote(down); got != "not running — factory up acme" {
		t.Fatalf("down note = %q", got)
	}

	if err := factory.Hold("acme", "test"); err != nil {
		t.Fatal(err)
	}
	if got := lineNote(down); got != "held by the andon cord — factory up acme releases it" {
		t.Fatalf("held note = %q", got)
	}
}

// A one-shot line has no resident session, so an empty section is its healthy
// resting state rather than a fault — and calling it "not running" was wrong
// under a header that had just said when it last beat.
func TestOneShotSectionIsNotReadAsDown(t *testing.T) {
	home := isolate(t)
	inst := factory.Instance{Name: "charlie", Runtime: factory.RuntimeOneShot}

	// Never started: the one case where `factory up` is the right advice.
	if got, want := lineNote(inst), "no beat yet — factory up charlie"; got != want {
		t.Fatalf("before any beat = %q, want %q", got, want)
	}

	// Has beaten, nothing in flight: between beats, and there is deliberately
	// no command offered, because there is nothing to fix.
	beats := filepath.Join(home, ".factory", "beats")
	if err := os.MkdirAll(beats, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beats, "charlie.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := lineNote(inst)
	if !strings.Contains(got, "between beats") {
		t.Fatalf("between beats = %q", got)
	}
	if strings.Contains(got, "factory up") {
		t.Errorf("a healthy one-shot line should offer no command, got %q", got)
	}

	// Mid-beat: the lock is the same signal `factory whoami` reads, so the
	// picker and the front desk cannot disagree about this.
	lock := filepath.Join(home, ".factory", "iterations", "charlie", "lock")
	if err := os.MkdirAll(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := lineNote(inst); !strings.Contains(got, "in flight") {
		t.Fatalf("mid-beat = %q", got)
	}
}

func TestSectionSeparatorRendersNameAndDetail(t *testing.T) {
	row := Row{Kind: KindSeparator, Name: "acme", Detail: "hev/acme · beat 5m ago"}
	got := row.render(defaultWidth, columns{})
	for _, want := range []string{"── acme", " · hev/acme · beat 5m ago ──"} {
		if !strings.Contains(got, want) {
			t.Fatalf("separator %q is missing %q", got, want)
		}
	}
}
