// Package factory reads the parts of a factory that a viewer needs: which
// instances are configured, which tmux sessions belong to them, and what the
// child ledger says about the workers they dispatched.
//
// Everything here reads. The gaffer writes the ledger and stamps PR state into
// it; a viewer never calls the network and never actuates.
package factory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RootPointer is where ./factory records which checkout it booted from. An
// installed binary lives in ~/golang/bin or /usr/local/bin with no repo above
// it, so it cannot find the configs by looking around itself — the factory has
// to say where it is.
func RootPointer() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".factory", "root")
}

// Root is the factory repo checkout — the directory holding `factory` and
// `factories/`. It is looked for in four places, most explicit first:
//
//  1. FACTORY_ROOT, for pointing one binary at another checkout;
//  2. two levels up from the executable, which is the in-repo build;
//  3. ~/.factory/root, written by ./factory, which is the installed build;
//  4. a walk up from the working directory, which covers `go run` and a
//     checkout that has never been booted.
func Root() string {
	if root := os.Getenv("FACTORY_ROOT"); IsRoot(root) {
		return root
	}
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			if root := filepath.Dir(filepath.Dir(exe)); IsRoot(root) {
				return root
			}
		}
	}
	if recorded, err := os.ReadFile(RootPointer()); err == nil {
		if root := strings.TrimSpace(string(recorded)); IsRoot(root) {
			return root
		}
	}
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; ; {
			if IsRoot(dir) {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}

// IsRoot is the marker: a factory checkout is a directory with factories/ in
// it. Nothing else on the machine looks like that.
func IsRoot(dir string) bool {
	if dir == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, "factories"))
	return err == nil && fi.IsDir()
}

// RecordRoot writes the pointer that lets a binary with no repo above it find
// the checkout. `./factory` writes the same file on every boot; an installed
// binary writes it once, when it clones the checkout it is going to use.
func RecordRoot(dir string) error {
	pointer := RootPointer()
	if pointer == "" {
		return fmt.Errorf("no home directory, so there is nowhere to record the checkout")
	}
	if err := os.MkdirAll(filepath.Dir(pointer), 0o755); err != nil {
		return err
	}
	return os.WriteFile(pointer, []byte(dir+"\n"), 0o644)
}
