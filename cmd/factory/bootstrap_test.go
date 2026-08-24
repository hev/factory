package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hev/factory/pkg/factory"
)

// newCheckout writes the one thing that makes a directory a factory checkout.
func newCheckout(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "factories"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCheckoutTargetDefaultsToWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(checkoutEnv, "")

	want := filepath.Join(home, "workspace", "factory")
	if got := checkoutTarget(); got != want {
		t.Errorf("checkoutTarget() = %q, want %q", got, want)
	}
}

func TestCheckoutTargetHonoursEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(checkoutEnv, "/somewhere/else")

	if got := checkoutTarget(); got != "/somewhere/else" {
		t.Errorf("checkoutTarget() = %q, want the override", got)
	}
}

// A machine that already has a checkout must never be asked anything, which is
// every run but the first and every run of an in-repo build.
func TestEnsureRootPassesThroughAnExistingRoot(t *testing.T) {
	var out strings.Builder
	root, cloned, err := ensureRoot("/already/here", strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	if root != "/already/here" || cloned != "" {
		t.Errorf("ensureRoot() = (%q, %q), want the root through and no clone", root, cloned)
	}
	if out.Len() != 0 {
		t.Errorf("said %q to somebody who asked for nothing", out.String())
	}
}

// Cloned by hand, never booted: `./factory` is what records the pointer, so
// until then an installed binary cannot see the checkout sitting right there.
// Adopting it beats cloning a second copy alongside it.
func TestEnsureRootAdoptsAnExistingCheckout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	target := newCheckout(t, filepath.Join(home, "workspace", "factory"))
	t.Setenv(checkoutEnv, target)

	var out strings.Builder
	root, cloned, err := ensureRoot("", strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	if root != target {
		t.Errorf("ensureRoot() root = %q, want %q", root, target)
	}
	if cloned != "" {
		t.Error("adopting a checkout is not cloning one, so nothing should boot")
	}

	recorded, err := os.ReadFile(factory.RootPointer())
	if err != nil {
		t.Fatalf("adopted the checkout but recorded no pointer: %v", err)
	}
	if strings.TrimSpace(string(recorded)) != target {
		t.Errorf("pointer = %q, want %q", strings.TrimSpace(string(recorded)), target)
	}
}

// Something is already at the target and it is not a checkout. Cloning into it
// is not an option and neither is deleting it, so it has to be said out loud.
func TestEnsureRootRefusesAnOccupiedTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	target := filepath.Join(home, "workspace", "factory")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(checkoutEnv, target)

	var out strings.Builder
	if _, _, err := ensureRoot("", strings.NewReader(""), &out); err == nil {
		t.Fatal("cloned over a directory that was already there")
	} else if !strings.Contains(err.Error(), checkoutEnv) {
		t.Errorf("error %q does not name %s, so nobody knows how to move it", err, checkoutEnv)
	}
}

// "n" is a real answer, and the error it produces is the command to run later.
func TestEnsureRootDeclinedNamesTheCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(checkoutEnv, filepath.Join(home, "workspace", "factory"))
	t.Setenv(repoEnv, "https://example.invalid/repo")

	var out strings.Builder
	_, _, err := ensureRoot("", strings.NewReader("n\n"), &out)
	if err == nil {
		t.Fatal("declined the clone and carried on anyway")
	}
	if !strings.Contains(err.Error(), "git clone https://example.invalid/repo") {
		t.Errorf("error %q does not say what to type", err)
	}
	if !strings.Contains(out.String(), "[Y/n]") {
		t.Error("cloned without asking")
	}
}

// No terminal to ask at — launchd, cron, a pipe. Prompting into the void and
// taking silence for yes is how a machine ends up with a repo nobody asked for.
func TestEnsureRootWithNothingToAskAt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(checkoutEnv, filepath.Join(home, "workspace", "factory"))
	t.Setenv(repoEnv, "https://example.invalid/repo")

	var out strings.Builder
	_, _, err := ensureRoot("", strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("no answer available, and it cloned anyway")
	}
	if !strings.Contains(err.Error(), "git clone") {
		t.Errorf("error %q does not say what to type", err)
	}
}
