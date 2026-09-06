package factory

import (
	"os"
	"path/filepath"
)

// A host can decline to be a desk.
//
// Reception is a conversation somebody opens in a workspace checkout, and the
// machine a factory runs on is usually not where anybody is sitting: it is a
// mini in a cupboard whose only voices should be the gaffers' records and, on
// a build that has one, the foreman. `~/.factory/no-desk` says so. `factory
// whoami` reports it and stops, and the picker offers no door
// (contracts/reception-charter.md).
//
// Existence is the whole signal, like a hold. FACTORY_NO_DESK=1 says the same
// thing from the environment, for a test or a one-off shell.

// NoDeskPath is the marker file.
func NoDeskPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".factory", "no-desk")
}

// NoDesk reports whether this machine has declined the desk.
func NoDesk() bool {
	if v := os.Getenv("FACTORY_NO_DESK"); v != "" && v != "0" {
		return true
	}
	path := NoDeskPath()
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
