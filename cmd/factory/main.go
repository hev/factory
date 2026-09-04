// Command factory is the factory's front door.
//
//	factory                the picker: every line's gaffers and workers, live
//	factory --list         print those rows once and exit (debugging)
//	factory init           write one factory's config and workspace hook
//	factory adopt <name>   add the reception hook to a configured workspace
//	factory whoami         identify the factory owning the current directory
//	factory logins         the credentials the agents run on, and their expiry
//	factory up [<name>]    bring up every gaffer registered on this machine
//	factory stop [<name>]  the andon cord: stop the gaffers
//
// This is the front door, not the boot sequence: ./factory in the checkout is
// the shell script that installs reception and starts the gaffers, and it
// execs its own local build of this binary when it is done.
//
// One screen is the whole floor. A machine running two factories opens on both
// at once, each line a section headed by its config-and-beat detail, and esc
// narrows to one line when one is all that matters. It lists the factories'
// gaffers and the sub-agents they dispatched, and nothing else — other tmux
// sessions are somebody's own work, and a factory's front door is the wrong
// place to switch to them.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hev/factory/internal/picker"
	"github.com/hev/factory/internal/stopline"
	"github.com/hev/factory/internal/tmuxctl"
	"github.com/hev/factory/pkg/factory"
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
	case "logins":
		// Answered before the checkout is located: whether `gh` is still logged
		// in is a question about the machine, not about a factory, and it has
		// to be answerable on one that has none configured.
		return runLogins(args[1:])
	case "", "--login", "--list", "init", "adopt", "whoami", "cleanup", "list", "up", "stop", "stop-the-line":
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
	case "adopt":
		return runAdopt(root, args[1:])
	case "whoami":
		return runWhoami(root, args[1:])
	case "cleanup":
		return runCleanup(root, args[1:])
	case "list":
		return runList(root, args[1:])
	case "up":
		return runUp(root, args[1:])
	case "stop", "stop-the-line":
		return runStop(root, args[1:])
	case "--list":
		fmt.Print(picker.Rows(root))
		return nil
	case "--login":
		return runLoginPicker(root)
	}
	return runPicker(root)
}

// runStop is the andon cord from a shell. It stops the gaffers, and nothing
// else — the workers keep whatever they are holding, and with no gaffer up
// nothing dispatches to them again. Naming an instance narrows it to that one
// factory's gaffer.
//
// There is no --all. It used to mean "every gaffer and every worker", which
// read as a bigger hammer than the default and was the one spelling that could
// lose somebody's in-flight branch. The cord has one reach now, so it needs no
// flag to say which reach you meant.
//
// Whatever it reaches, it also holds down: the boot fire runs every 300s and
// would otherwise undo the stop before you had finished reading it.
func runStop(root string, args []string) error {
	instance := ""
	switch arg := first(args); arg {
	case "":
	case "--all":
		return fmt.Errorf("--all is gone: `factory stop` stops the gaffers, and workers are stopped one at a time in the picker (^x)")
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
	return stopline.Run(root, instance)
}

// runUp is the other half of the cord: it lifts the holds, hands the boot back
// to the shell script — which is where starting a gaffer actually lives — and
// then says, per factory, whether the gaffer is up.
//
// The saying-so is the point. `up` used to end wherever the boot script's last
// line landed, which meant the answer to "is the line running?" was a guess
// from scrollback. This is the docker-compose shape instead: one row per
// factory, its state, and a count. You run `factory up` and you know.
//
// It execs `./factory --no-picker` rather than reimplementing the boot, so
// there is exactly one boot path and `factory up` cannot drift from it.
//
// Naming an instance releases just that one; bare `up` releases every factory
// configured here, which is what "bring the line back" means.
func runUp(root string, args []string) error {
	instance := ""
	switch arg := first(args); arg {
	case "":
	default:
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("unknown up argument %q", arg)
		}
		if !configured(root, arg) {
			return fmt.Errorf("no factory named %q — see factory list", arg)
		}
		instance = arg
	}

	// What the run is about to touch, and what it looked like beforehand — a
	// gaffer that was already up is "running", not "started", and the
	// difference is the whole reason to read the table.
	type target struct {
		name   string
		wasUp  bool
		held   bool
		status string
	}
	var targets []target
	for _, inst := range factory.LoadInstances(root) {
		if instance != "" && inst.Name != instance {
			continue
		}
		targets = append(targets, target{
			name:  inst.Name,
			wasUp: tmuxctl.HasSession(factory.GafferFor(inst.Name)),
			held:  factory.IsHeld(inst.Name),
		})
		if err := factory.Release(inst.Name); err != nil {
			return fmt.Errorf("could not lift the hold on %s: %w", inst.Name, err)
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("no factories configured here — run factory init")
	}

	fmt.Printf("[+] Up %d factor%s\n", len(targets), pluralY(len(targets)))

	// The boot's own chatter is the build log, not the answer. It goes to
	// stderr so the table below stays the thing you read.
	boot := exec.Command("/bin/bash", filepath.Join(root, "factory"), "--no-picker")
	boot.Stdout, boot.Stderr = os.Stderr, os.Stderr
	bootErr := boot.Run()

	up := 0
	for i := range targets {
		t := &targets[i]
		switch {
		case !tmuxctl.HasSession(factory.GafferFor(t.name)):
			t.status = "failed to start"
		case t.wasUp:
			t.status = "running"
			up++
		default:
			t.status = "started"
			up++
		}
		if t.held {
			t.status += " (hold lifted)"
		}
	}

	for _, t := range targets {
		mark := "✔"
		if strings.HasPrefix(t.status, "failed") {
			mark = "✘"
		}
		fmt.Printf(" %s %-16s %-14s %s\n", mark, t.name, factory.GafferFor(t.name), t.status)
	}
	fmt.Printf("[+] Ready %d/%d\n", up, len(targets))

	if up < len(targets) {
		if bootErr != nil {
			return fmt.Errorf("boot failed: %w", bootErr)
		}
		return fmt.Errorf("%d gaffer(s) did not come up — see the boot output above", len(targets)-up)
	}
	return bootErr
}

// pluralY is "factory"/"factories"; list.go's plural is the -s one.
func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func runPicker(root string) error {
	action, err := picker.Run(root)
	if err != nil {
		return err
	}
	return performAction(action)
}

// performAction is what the picker decided, carried out once the terminal is
// this process's again. Attaching replaces the process, so none of this can
// happen while bubbletea still owns the screen.
func performAction(action picker.Action) error {
	switch action.Kind {
	case picker.ActionConnect:
		return tmuxctl.Connect(action.Name)
	case picker.ActionDesk:
		return openDesk(action)
	}
	return nil
}

// openDesk hands the terminal to a front desk, starting one if there is not
// one already.
//
// The session is created here rather than inside the picker on purpose. The
// TUI still never starts an agent — it decided *which* desk and gave the
// terminal back, and this is the same place the andon cord's actuation and
// every attach already live. It also means a desk that fails to start fails at
// a shell, with the error on stderr, rather than inside an alt-screen that is
// about to be torn down.
//
// The command is `claude /reception` rather than a bare `claude`. A configured
// workspace has a SessionStart hook that runs `factory whoami` and tells the
// session to load the skill, so a bare claude would usually work — and would
// silently open an ordinary coding session in a workspace whose hook was never
// installed. Naming the skill fails loudly instead, which is the failure worth
// having.
func openDesk(action picker.Action) error {
	if err := tmuxctl.NewSession(action.Name, action.Dir, "claude", "/reception"); err != nil {
		return fmt.Errorf("could not open %s: %w", action.Name, err)
	}
	return tmuxctl.Connect(action.Name)
}

// runLoginPicker is the picker as a login shell's landing screen, and differs
// from the plain one in exactly one way: leaving it is an error.
//
// The loop it belongs in is
//
//	while factory --login; do :; done
//
// which exists so that detaching from a session puts you back on the floor
// rather than at a bare prompt. Attaching cannot end that loop, because
// attaching replaces this process with tmux and the shell only sees tmux's own
// exit — so the *only* thing that can end it is the picker returning with
// nothing picked, and the only way the shell can tell that apart from a
// detach is a status code.
//
// So ^c and esc exit 130, the conventional "the operator interrupted this",
// which is also what the shell picker this replaced produced by letting SIGINT
// through fzf. Plain `factory` is left alone: an exit code that means "you
// closed the screen you opened" would be a lie everywhere else.
func runLoginPicker(root string) error {
	action, err := picker.Run(root)
	if err != nil {
		return err
	}
	if action.Kind == picker.ActionNone {
		os.Exit(130)
	}
	return performAction(action)
}

func first(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

const usage = `factory — the factory's front door

  factory             the picker: every line's gaffers and sub-agents, live,
                      sectioned by factory · esc narrows to one line
  factory --login     the same screen as a login shell's landing page. Leaving
                      it exits 130, so a "while factory --login" loop puts you
                      back on the floor after a detach and still lets ^c out
  factory list        the factories configured here, and what is up
  factory --list      print the picker's rows once and exit
  factory init        write one factory's config (factory init --help)
  factory adopt NAME  install the reception hook in its existing workspace
  factory whoami      identify the factory for the current directory
  factory logins      the credentials the agents run on, and when they expire
                      (--live also asks github and 1password)
  factory cleanup     remove a factory and its remnants (factory cleanup --help)
  factory up          bring up every gaffer registered on this machine, and
                      report which ones are ready
  factory up <name>   the same, for one factory on a machine running several
  factory stop        the andon cord: stop the gaffers and hold them down until
                      the next factory up. Workers keep running — nothing
                      dispatches to them with the gaffers down
  factory stop <name> the same, for one factory on a machine running several

keys
  ↵            attach to the highlighted row (on the stop-the-line row, stop
               this factory's sub-agents — confirms first)
  ^d, →, ←     open or close the detail panel on the highlighted row
  ^g           tell the gaffer about the highlighted sub-agent
  ^x           stop the highlighted sub-agent (confirms first)
  ^a           the logins screen: every credential and when it expires
  ^r           refresh now
  type         filter · esc clears the filter, then opens the floor chooser
               on a machine with several factories, then leaves

environment
  FACTORY_ROOT              the factory checkout (default: ~/.factory/root)
  FACTORY_CHECKOUT          where an installed binary clones one on first run
                            (default: ~/workspace/factory)
  FACTORY_REPO              what it clones (default: github.com/hev/factory)
  FACTORY_LEDGER_DIR        child ledger (default: ~/.factory/children)
  LEDGER_STALE_HOURS        hours before a PR-less worker is flagged (default: 4)
  FACTORY_INSTANCE_COLORS   pin instance accents, e.g. "acme=#89b4fa,docs=#fab387"
  FACTORY_AUTH_EXPIRY       expiry dates nothing on the machine records,
                            e.g. "1password=2026-11-03"
`
