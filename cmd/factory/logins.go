package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/hev/factory/internal/auth"
	"github.com/hev/factory/internal/ui"
)

// runLogins prints the credential reading once and exits — the shell form of
// the picker's ^a screen, and the way to check it from a script, a cron job or
// an ssh session that never opens a TUI.
//
// `--live` adds the network probes. They are opt-in here for the same reason
// they are behind a key in the picker: they go to the network, and `op whoami`
// on a headless Mac has been measured not answering at all.
func runLogins(args []string) error {
	live := false
	for _, arg := range args {
		switch arg {
		case "--live":
			live = true
		default:
			return fmt.Errorf("unknown logins argument %q\n\nusage: factory logins [--live]", arg)
		}
	}

	creds := auth.Check()
	if live {
		creds = auth.Probe(creds)
	}

	attention := 0
	for _, c := range creds {
		mark := " "
		if c.Attention() {
			mark = "!"
			attention++
		}
		fmt.Printf(" %s %-12s %-26s %-14s %-15s %s\n",
			mark, c.Name, c.What, c.State, expiry(c), c.Detail)
	}
	if attention > 0 {
		return fmt.Errorf("%d credential(s) need attention", attention)
	}
	return nil
}

// expiry is the date in the direction that matters, or a dash when nothing on
// this machine records one. A dash is an answer; a guess would not be.
func expiry(c auth.Credential) string {
	if c.Expires.IsZero() {
		return "-"
	}
	phrase := ""
	if left := time.Until(c.Expires); left > 0 {
		phrase = "in " + ui.Duration(int(left.Seconds()))
	} else {
		phrase = ui.Duration(int(-left.Seconds())) + " ago"
	}
	if c.Pinned {
		phrase += " (set)"
	}
	return strings.TrimSpace(phrase)
}
