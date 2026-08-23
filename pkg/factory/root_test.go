package factory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootPrefersTheExplicitAnswer(t *testing.T) {
	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "factories"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FACTORY_ROOT", checkout)
	if got := Root(); got != checkout {
		t.Errorf("Root() = %q, want the FACTORY_ROOT checkout %q", got, checkout)
	}

	// A path that is not a checkout is ignored rather than trusted, so a stale
	// env var degrades to the other lookups instead of blanking the screen.
	t.Setenv("FACTORY_ROOT", filepath.Join(checkout, "nope"))
	if got := Root(); got == filepath.Join(checkout, "nope") {
		t.Error("a FACTORY_ROOT with no factories/ in it should not be believed")
	}
}

func TestRootReadsThePointerFile(t *testing.T) {
	home := t.TempDir()
	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "factories"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".factory"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Trailing newline included: ./factory writes this with echo.
	if err := os.WriteFile(filepath.Join(home, ".factory", "root"), []byte(checkout+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("FACTORY_ROOT", "")
	// Run from somewhere with no checkout above it, the way an installed
	// binary is usually invoked.
	t.Chdir(t.TempDir())

	if got := Root(); got != checkout {
		t.Errorf("Root() = %q, want the recorded checkout %q", got, checkout)
	}
}
