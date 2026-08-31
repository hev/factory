package picker

import "testing"

func TestEmptyFloorHasNoReceptionRow(t *testing.T) {
	if got := emptyNote("acme"); got != "nothing dispatched for acme — run ./factory" {
		t.Fatalf("empty floor note = %q", got)
	}
	if got := emptyNote(""); got != "no factory configured — run ./factory init" {
		t.Fatalf("bootstrap note = %q", got)
	}
}
