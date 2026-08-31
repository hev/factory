package factory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHoldReleaseRoundTrip(t *testing.T) {
	t.Setenv("FACTORY_HOLDS_DIR", t.TempDir())

	if IsHeld("lyr") {
		t.Fatal("a fresh machine holds nothing")
	}
	if err := Hold("lyr", "test"); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	if !IsHeld("lyr") {
		t.Fatal("held instance did not read back as held")
	}
	if IsHeld("charlie") {
		t.Fatal("a hold on one factory must not hold another")
	}

	// The cord is pullable twice.
	if err := Hold("lyr", "test again"); err != nil {
		t.Fatalf("second Hold: %v", err)
	}

	if err := Release("lyr"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if IsHeld("lyr") {
		t.Fatal("released instance still reads as held")
	}
	// Releasing what is not held is success, not an error.
	if err := Release("lyr"); err != nil {
		t.Fatalf("second Release: %v", err)
	}
}

// The boot script tests the hold with `[[ -e ]]`, so existence has to be the
// whole signal — a hold must never depend on what is written inside it.
func TestHoldIsSignalledByExistenceAlone(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FACTORY_HOLDS_DIR", dir)

	if err := os.WriteFile(filepath.Join(dir, "lyr"), nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !IsHeld("lyr") {
		t.Fatal("an empty hold file is still a hold")
	}
}

func TestHoldPathEmptyInstance(t *testing.T) {
	t.Setenv("FACTORY_HOLDS_DIR", t.TempDir())
	if got := HoldPath(""); got != "" {
		t.Fatalf("HoldPath(%q) = %q, want empty — an unnamed factory has no hold", "", got)
	}
	if IsHeld("") {
		t.Fatal(`IsHeld("") must be false`)
	}
}
