package picker

import "testing"

// The desk row exists in both states. The regression this guards is a floor
// with sub-agents on it and no reception line at all: the desk had gone down,
// and the only thing that said so was a row that had stopped being drawn.
func TestDeskRow(t *testing.T) {
	up := deskRow("acme", true)
	if up.Name != "factory-acme" || up.Kind != KindReception || !up.Up {
		t.Fatalf("desk row for a live desk: %+v", up)
	}

	down := deskRow("acme", false)
	if down.Name != "factory-acme" || down.Up {
		t.Fatalf("desk row for a down desk: %+v", down)
	}
	if down.Detail == up.Detail {
		t.Fatal("a down desk reads the same as one on duty")
	}
	if !down.selectable() {
		t.Fatal("a down desk cannot be selected, so ↵ cannot put it back")
	}

	// The bootstrap desk on a machine with nothing configured is named for no
	// instance, because there is not one to name it after.
	if boot := deskRow("", false); boot.Name != "reception" {
		t.Fatalf("bootstrap desk session = %q", boot.Name)
	}
}

// Both states render, and a filter for "reception" finds either.
func TestDeskRowRenders(t *testing.T) {
	for _, up := range []bool{true, false} {
		row := deskRow("acme", up)
		if row.render(120, planColumns(nil, 120)) == "" {
			t.Fatalf("up=%v rendered nothing", up)
		}
		if !matches(row.Search, "recep") {
			t.Fatalf("up=%v does not match a filter for reception", up)
		}
	}
}
