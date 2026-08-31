// Package stopline is the andon cord — named for the one over a Toyota
// assembly line that anyone on the floor can pull.
//
// It stops the gaffers: the agent in each gaffer session gets TERM so it can
// shut down cleanly, and then that session is killed. Stopping the gaffers is
// what stops the line, because a gaffer is the only thing that dispatches
// work — with them down, no new worker is started and the factory makes no
// further decisions.
//
// It deliberately leaves the workers running. A worker is somebody's task
// mid-flight, usually a branch with uncommitted work on it, and killing a
// dozen of those to halt the line loses more than it saves. A worker you
// actually want gone is one row and one ^x in the picker.
//
// It reaches nothing else. A Mac that runs a factory is usually also a Mac
// somebody works on, and halting the line should not take their editor down
// with it — so an agent the factory never started is not the cord's to stop,
// however much it looks like one from the outside.
package stopline

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hev/factory/internal/tmuxctl"
	"github.com/hev/factory/internal/ui"
	"github.com/hev/factory/pkg/factory"
)

// gracePeriod is how long an agent gets between TERM and having its session
// killed out from under it. Long enough to flush a transcript, short enough
// that pulling the cord still feels like pulling a cord.
const gracePeriod = 2 * time.Second

// Target is one gaffer session and what is running inside it.
type Target struct {
	Session string
	Kind    factory.Kind
	Agents  []factory.AgentProc
}

// Report is what the cord found, and what pulling it would do.
type Report struct {
	Root     string   // the checkout, for resolving which factories a hold covers
	Instance string   // which factory, "" for every one on the machine
	Targets  []Target // the gaffer sessions to stop
	numAgent int
}

// Agents is how many agent processes the cord would signal.
func (r Report) Agents() int { return r.numAgent }

// Sessions is how many gaffer sessions the cord would kill.
func (r Report) Sessions() int { return len(r.Targets) }

// Empty reports whether there is nothing to stop.
func (r Report) Empty() bool { return r.numAgent == 0 && r.Sessions() == 0 }

// Scan works out what the cord would reach, without touching anything. An
// instance narrows it to one factory's gaffer; "" is every factory on the
// machine, which is what the shell cord means by default.
func Scan(root string, instance string) Report {
	procs := factory.Snapshot()
	report := Report{Root: root, Instance: instance}

	// Group panes by session, so an agent in any window of a factory session
	// is found, then keep only the sessions the factory owns.
	panesBySession := map[string][]int{}
	for _, pane := range tmuxctl.AllPanes() {
		panesBySession[pane.Session] = append(panesBySession[pane.Session], pane.PID)
	}

	fleet := factory.NewScope(root)
	now := time.Now()
	for _, session := range tmuxctl.ListSessions() {
		member := fleet.Classify(session.Name, now)
		// Gaffers only. A worker the cord skips keeps its branch and its
		// half-finished edit; with the gaffer gone, nothing dispatches to it
		// again.
		if member.Kind != factory.Gaffer {
			continue
		}
		if instance != "" && member.Instance != instance {
			continue
		}
		target := Target{Session: session.Name, Kind: member.Kind}
		for _, panePID := range panesBySession[session.Name] {
			target.Agents = append(target.Agents, procs.AgentsIn(panePID)...)
		}
		report.numAgent += len(target.Agents)
		report.Targets = append(report.Targets, target)
	}

	sort.SliceStable(report.Targets, func(i, j int) bool {
		return report.Targets[i].Session < report.Targets[j].Session
	})
	return report
}

// hold marks every factory this pull covers as deliberately down, so the next
// `./factory` leaves it alone until somebody runs `factory up`.
func (r Report) hold() {
	note := fmt.Sprintf("stopped by the andon cord at %s", time.Now().Format(time.RFC3339))
	for _, name := range r.instances() {
		_ = factory.Hold(name, note)
	}
}

// instances is which factories the cord covers: the one it was aimed at, or
// every one configured on this machine. It reads the configs rather than the
// targets, because a factory whose gaffer is already down still has to be held
// — otherwise "stop" on a stopped factory quietly means "start it in five
// minutes".
func (r Report) instances() []string {
	if r.Instance != "" {
		return []string{r.Instance}
	}
	var out []string
	for _, inst := range factory.LoadInstances(r.Root) {
		out = append(out, inst.Name)
	}
	return out
}

// Stop pulls the cord and reports what it reached.
//
// TERM first, everywhere, then a pause, then the sessions. An agent that is
// mid-write gets its chance to finish before its tmux session disappears.
//
// The hold goes on first, and it goes on whether or not anything was running.
// The 300s boot fire is the thing most likely to undo this, and it can land
// between the TERM and the last kill — so the file that tells it to stay away
// is written before any of that starts, and stopping an already-stopped
// factory is a real act rather than a no-op.
func Stop(report Report) (agents, sessions int) {
	report.hold()

	term := func(list []factory.AgentProc) {
		for _, agent := range list {
			if syscall.Kill(agent.PID, syscall.SIGTERM) == nil {
				agents++
			}
		}
	}

	for _, target := range report.Targets {
		term(target.Agents)
	}
	if agents > 0 {
		time.Sleep(gracePeriod)
	}
	for _, target := range report.Targets {
		// Gone is gone, however it went. TERM-ing an agent that *is* the pane
		// process ends its session on the spot, so kill-session then fails
		// with "no such session" — which is success, not an error.
		_ = tmuxctl.KillSession(target.Session)
		if !tmuxctl.HasSession(target.Session) {
			sessions++
		}
	}
	return agents, sessions
}

// Summary is the one-line description of what the cord found, for a confirm.
func (r Report) Summary() string {
	switch {
	case r.numAgent == 0:
		return fmt.Sprintf("%d gaffer(s), nothing running in them", len(r.Targets))
	default:
		return fmt.Sprintf("%d agent(s) in %d gaffer(s)", r.numAgent, len(r.Targets))
	}
}

// Lines describes each thing the cord would stop, one per line.
func (r Report) Lines() []string {
	out := make([]string, 0, len(r.Targets))
	for _, target := range r.Targets {
		detail := "no agent"
		if n := len(target.Agents); n > 0 {
			detail = fmt.Sprintf("%d agent(s)", n)
		}
		out = append(out, fmt.Sprintf("%-18s %-9s %s", target.Session, kindName(target.Kind), detail))
	}
	return out
}

func kindName(kind factory.Kind) string {
	switch kind {
	case factory.Gaffer:
		return "gaffer"
	case factory.Worker:
		return "worker"
	}
	return ""
}

// Run is the standalone cord, for when it is pulled from a shell rather than
// from a row in the picker.
func Run(root string, instance string) error {
	report := Scan(root, instance)

	// Nothing running is still worth a hold: the boot fire would otherwise
	// start it back up within five minutes, and an operator who just typed
	// `factory stop` has said plainly that they do not want that.
	if report.Empty() {
		report.hold()
		fmt.Println("  " + ui.Dim.Render("Nothing running. The line is already stopped, and held down."))
		fmt.Println("  " + ui.Dim.Render(heldBy(report)))
		return nil
	}

	fmt.Println()
	fmt.Println("  " + ui.Alarm.Render("🚨 STOP THE LINE"))
	fmt.Println("  " + ui.Header.Render(report.Summary()+":"))
	for _, line := range report.Lines() {
		fmt.Println("    " + line)
	}
	fmt.Println("  " + ui.Dim.Render("TERM, then the sessions. Workers keep running; nothing dispatches to them."))
	fmt.Println("  " + ui.Dim.Render("They stay down until `factory up`."))
	fmt.Print("\n  " + ui.Alarm.Render("Stop the line? [y/N] "))

	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		agents, sessions := Stop(report)
		fmt.Println("  " + ui.Flash.Render(fmt.Sprintf("✓ Line stopped — %s", result(agents, sessions))))
		fmt.Println("  " + ui.Dim.Render(heldBy(report)))
	default:
		fmt.Println("  " + ui.Dim.Render("Cancelled. The line keeps running."))
	}
	return nil
}

// heldBy names what the boot fire will now skip, because a hold the operator
// cannot see is a factory that mysteriously never starts.
func heldBy(r Report) string {
	names := r.instances()
	if len(names) == 0 {
		return "No factories configured, so nothing is held."
	}
	return fmt.Sprintf("Held: %s. `factory up` starts them again.", strings.Join(names, ", "))
}

// result phrases what a pull actually did.
func result(agents, sessions int) string {
	if sessions == 0 {
		return fmt.Sprintf("%d gaffer(s) sent TERM", agents)
	}
	return fmt.Sprintf("%d agent(s) sent TERM, %d gaffer(s) closed", agents, sessions)
}
