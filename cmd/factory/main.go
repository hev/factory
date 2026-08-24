// Command factory is the factory's front door.
//
//	factory                the picker: one factory's sub-agents, live
//	factory --list         print those rows once and exit (debugging)
//	factory init           write one factory's config (reception calls this)
//	factory stop [<name>]  the andon cord: stop a factory's sub-agents
//	factory stop --all     halt every agent on the machine, factory or not
//
// This is the front door, not the boot sequence: ./factory in the checkout is
// the shell script that puts reception on duty and starts the gaffers, and it
// execs its own local build of this binary when it is done.
//
// One screen is one factory. It lists that factory's reception, its gaffer,
// and the sub-agents the gaffer dispatched, and nothing else — a machine
// running two factories asks which one first. Other tmux sessions are
// somebody's own work, and a factory's front door is the wrong place to switch
// to them.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/hev/factory/pkg/factory"
	"github.com/hev/factory/internal/picker"
	"github.com/hev/factory/internal/stopline"
	"github.com/hev/factory/internal/tmuxctl"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "factory: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := first(args)

	// Settled before anything goes looking for a checkout. --help has to work
	// on a machine with nothing set up, and a typo should be a typo rather than
	// a reason to offer to clone a repo into somebody's home directory.
	switch cmd {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	case "", "--list", "init", "cleanup", "list", "stop", "stop-the-line":
	default:
		return fmt.Errorf("unknown argument %q\n\n%s", args[0], usage)
	}

	root, cloned, err := ensureRoot(factory.Root(), os.Stdin, os.Stderr)
	if err != nil {
		return err
	}
	// A clone is the setup run, and bare `factory` is the setup command: hand
	// over to the checkout's own boot sequence. Anything more specific was
	// asked for by name and is answered by name.
	if cloned != "" && cmd == "" {
		return bootFreshCheckout(cloned, os.Stderr)
	}

	switch cmd {
	case "init":
		return runInit(root, args[1:])
	case "cleanup":
		return runCleanup(root, args[1:])
	case "list":
		return runList(root, args[1:])
	case "stop", "stop-the-line":
		return runStop(root, args[1:])
	case "--list":
		fmt.Print(picker.Rows(root))
		return nil
	}
	return runPicker(root)
}

// runStop is the andon cord from a shell. It stops the factory's sub-agents
// and leaves every reception desk standing. Naming an instance stops that one
// factory; the machine-wide sweep is behind --all, because halting the line
// should not take down an editor somebody is typing in.
func runStop(root string, args []string) error {
	scope, instance := stopline.Factory, ""
	switch arg := first(args); arg {
	case "--all":
		scope = stopline.All
	case "":
	default:
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("unknown stop argument %q", arg)
		}
		// A typo stops nothing rather than everything.
		if !configured(root, arg) {
			return fmt.Errorf("no factory named %q — see factory list", arg)
		}
		instance = arg
	}
	return stopline.Run(root, scope, instance)
}

func runPicker(root string) error {
	action, err := picker.Run(root)
	if err != nil {
		return err
	}
	switch action.Kind {
	case picker.ActionDesk:
		// The desk was down and ↵ asked for it. Boot it here, with the screen
		// given back, so the wait and its output are visible rather than
		// happening behind a frozen TUI.
		if err := factory.ReceptionUp(root, action.Instance); err != nil {
			return err
		}
		return tmuxctl.Connect(action.Name)
	case picker.ActionConnect:
		return tmuxctl.Connect(action.Name)
	}
	return nil
}

func first(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

const usage = `factory — the factory's front door

  factory             the picker: one factory's sub-agents, live
  factory list        the factories configured here, and what is up
  factory --list      print the picker's rows once and exit
  factory init        write one factory's config (factory init --help)
  factory cleanup     remove a factory and its remnants (factory cleanup --help)
  factory stop        the andon cord: stop the sub-agents, keep the desk
  factory stop <name> the same, for one factory on a machine running several
  factory stop --all  halt every agent on the machine, factory or not

keys
  ↵            attach to the highlighted row
  ^x           stop the highlighted sub-agent (confirms first)
  ^r           refresh now
  type         filter · esc clears the filter, then leaves

environment
  FACTORY_ROOT              the factory checkout (default: ~/.factory/root)
  FACTORY_CHECKOUT          where an installed binary clones one on first run
                            (default: ~/workspace/factory)
  FACTORY_REPO              what it clones (default: github.com/hev/factory)
  FACTORY_LEDGER_DIR        child ledger (default: ~/.factory/children)
  LEDGER_STALE_HOURS        hours before a PR-less worker is flagged (default: 4)
  FACTORY_INSTANCE_COLORS   pin instance accents, e.g. "acme=#89b4fa,docs=#fab387"
`
