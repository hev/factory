package factory

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ReceptionUp puts a front desk on duty by running the same script the boot
// sequence runs — reception-up.sh in the checkout, idempotent, which creates
// the tmux session and starts claude in it under the charter.
//
// The picker calls this for ↵ on a desk that is down. It does not reimplement
// the boot: a desk started by the front door and a desk started by ./factory
// have to be the same desk, and the only way to guarantee that is for there to
// be one thing that starts them.
//
// An empty instance is the bootstrap desk, session `reception`, on a machine
// with no factory configured yet.
//
// It is slow on purpose — the script waits for claude to come up, and prints
// what it is doing — so callers should run it with the terminal in hand.
func ReceptionUp(root, instance string) error {
	script := filepath.Join(root, "reception-up.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("no reception-up.sh under %s — is FACTORY_ROOT the checkout?", root)
	}

	var args []string
	if instance != "" {
		args = append(args, instance)
	}
	cmd := exec.Command(script, args...)
	cmd.Dir = root
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not put reception on duty: %w", err)
	}
	return nil
}
