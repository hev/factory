package factory

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GafferMsg hands a gaffer something its operator noticed.
//
// Watching the floor is how you find out a worker is stuck, and until now the
// only things to do about it were to attach and steer it yourself or to stop
// it. Neither is what an operator usually wants: the gaffer dispatched that
// worker, holds the plan it came from, and is the thing that should decide
// whether to coach it, re-dispatch it, or leave it alone. Telling the gaffer
// is a smaller act than taking the work off it.
//
// It runs scripts/gaffer-msg.sh rather than writing the inbox file itself, for
// so a message the picker delivers and a message reception delivers are the
// same message, and
// the only way to guarantee that is for one thing to write them. The script
// drops a JSON file into ~/.factory/inbox/<instance>/, which the gaffer drains
// as step 0 of every beat.
//
// Priority is "steer", always. The picker is a screen for watching, and
// "interrupt" is documented as relaying an order whose value expires before
// the next beat — a judgement about urgency that belongs to whoever is talking
// to the operator, which is reception, not a list. The immediate controls on
// this screen stop a worker; this one tells somebody about it.
func GafferMsg(root, instance, msg string) error {
	if strings.TrimSpace(msg) == "" {
		return fmt.Errorf("nothing to send")
	}
	if instance == "" {
		return fmt.Errorf("no factory to send to — this machine has none configured")
	}

	script := filepath.Join(root, "scripts", "gaffer-msg.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("no scripts/gaffer-msg.sh under %s — is FACTORY_ROOT the checkout?", root)
	}

	cmd := exec.Command(script, instance, "steer", msg)
	cmd.Dir = root
	// The operator is the one typing, so the message is theirs and says so.
	// A gaffer weighs a line from the person who owns the factory differently
	// from one relayed by the desk, and it can only do that if it is told.
	cmd.Env = append(os.Environ(), "FACTORY_MSG_FROM=operator", "RELAYING_OPERATOR=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("gaffer-msg: %s", detail)
	}
	return nil
}
