package factory

import (
	"os"
	"path/filepath"
	"testing"
)

// A machine declines the desk with a file, or with the environment for a
// one-off; a fresh machine has one.
func TestNoDeskIsAFileOrTheEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("FACTORY_NO_DESK", "")
	if NoDesk() {
		t.Fatal("a fresh machine has a desk")
	}

	marker := filepath.Join(home, ".factory", "no-desk")
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("dedicated factory host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !NoDesk() {
		t.Fatal("~/.factory/no-desk should decline the desk")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FACTORY_NO_DESK", "1")
	if !NoDesk() {
		t.Fatal("FACTORY_NO_DESK=1 should decline the desk")
	}
	t.Setenv("FACTORY_NO_DESK", "0")
	if NoDesk() {
		t.Fatal("FACTORY_NO_DESK=0 is not a decline")
	}
}
