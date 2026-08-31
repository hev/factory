package factory

import (
	"os"
	"path/filepath"
)

// A hold is how the andon cord outlives the timer that would undo it.
//
// `./factory` is idempotent and runs on a 300s launchd fire, so it exists to
// bring back anything that is down — which is exactly what makes it the enemy
// of a stop. Before holds, pulling the cord bought you at most five minutes of
// quiet, and the gaffer came back on its own with no record of why it had ever
// gone. That is the worst kind of bug: the operator's explicit order silently
// loses to a scheduled one.
//
// So stopping writes a file per instance and booting deletes it. `factory`
// skips any instance that is held, which makes the pair mean what an operator
// already assumes it means — `factory stop` stays stopped, `factory up` starts
// it again. It is a file rather than a launchctl unload because the unit is one
// factory, not the machine: one gaffer can be held while another keeps running,
// and a reboot with a hold in place still comes up quiet.
//
// The file's contents are advisory — a timestamp and a reason, for whoever
// finds it later. Existence is the whole signal, so the shell script that reads
// it needs nothing but a `[[ -e ]]`.

// HoldsDir is where the per-instance hold files live.
func HoldsDir() string {
	if d := os.Getenv("FACTORY_HOLDS_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".factory", "holds")
}

// HoldPath is the hold file for one instance.
func HoldPath(instance string) string {
	dir := HoldsDir()
	if dir == "" || instance == "" {
		return ""
	}
	return filepath.Join(dir, instance)
}

// Hold marks one instance as deliberately stopped, so the next boot leaves it
// alone. Writing an existing hold again is not an error: the cord is pullable
// twice, and the second pull should not report a failure.
func Hold(instance, note string) error {
	path := HoldPath(instance)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(note+"\n"), 0o644)
}

// Release lifts the hold on one instance. A hold that is not there is already
// released, which is success.
func Release(instance string) error {
	path := HoldPath(instance)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IsHeld reports whether an instance is being kept down on purpose.
func IsHeld(instance string) bool {
	path := HoldPath(instance)
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// Held lists the instances currently held, in the order the configs are read,
// so a caller can say which factories a boot is going to skip.
func Held(root string) []string {
	var out []string
	for _, inst := range LoadInstances(root) {
		if IsHeld(inst.Name) {
			out = append(out, inst.Name)
		}
	}
	return out
}
