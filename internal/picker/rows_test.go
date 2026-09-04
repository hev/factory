package picker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hev/factory/pkg/factory"
)

func TestEmptyFloorHasNoReceptionRow(t *testing.T) {
	if got := emptyNote("acme"); got != "nothing dispatched for acme — run ./factory" {
		t.Fatalf("empty floor note = %q", got)
	}
	if got := emptyNote(""); got != "no factory configured — run ./factory init" {
		t.Fatalf("bootstrap note = %q", got)
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

	down := factory.Instance{Name: "acme"}
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

func TestSectionSeparatorRendersNameAndDetail(t *testing.T) {
	row := Row{Kind: KindSeparator, Name: "acme", Detail: "hev/acme · beat 5m ago"}
	got := row.render(defaultWidth, columns{})
	for _, want := range []string{"── acme", " · hev/acme · beat 5m ago ──"} {
		if !strings.Contains(got, want) {
			t.Fatalf("separator %q is missing %q", got, want)
		}
	}
}
