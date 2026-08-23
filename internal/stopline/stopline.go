// Package stopline is the andon cord — named for the one over a Toyota
// assembly line that anyone on the floor can pull.
//
// It stops the sub-agents: every agent running in a gaffer or worker session
// gets TERM so it can shut down cleanly, and then those sessions are killed.
// What it reaches is exactly what the picker lists above it, which is the
// point — a cord whose reach you have to reason about before pulling is not a
// cord.
//
// Reception is not on the cord. The desk is who you talk to about a line that
// just stopped, and a cord that takes out the person you would ask about it
// leaves you looking at a dead machine with nobody to ask.
//
// The machine-wide sweep is still available behind an explicit ask (Scope All),
// because sometimes a runaway agent is one the factory never started. It is not
// the default: a Mac that runs a factory is usually also a Mac somebody works
// on, and halting the line should not take their editor down with it.
package stopline

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hev/factory/pkg/factory"
	"github.com/hev/factory/internal/tmuxctl"
	"github.com/hev/factory/internal/ui"
)

// gracePeriod is how long an agent gets between TERM and having its session
// killed out from under it. Long enough to flush a transcript, short enough
// that pulling the cord still feels like pulling a cord.
const gracePeriod = 2 * time.Second

// Scope is how wide the cord reaches.
type Scope int

const (
	// Factory is the default: the sub-agents the picker lists.
	Factory Scope = iota
	// All is every agent on the machine, factory or not.
	All
)

// Target is one factory session and what is running inside it.
type Target struct {
	Session string
	Kind    factory.Kind
	Agents  []factory.AgentProc
}

// Report is what the cord found, and what pulling it would do.
type Report struct {
	Scope    Scope
	Instance string              // factory scope: which factory, "" for every one
	Targets  []Target            // factory scope: sub-agents to stop
	Loose    []factory.AgentProc // all scope: agents with no factory session
	numAgent int
}

// Agents is how many agent processes the cord would signal.
func (r Report) Agents() int { return r.numAgent }

// Sessions is how many sub-agent sessions the cord would kill. Zero under
// Scope All, which signals processes and leaves every session standing.
func (r Report) Sessions() int {
	if r.Scope == All {
		return 0
	}
	return len(r.Targets)
}

// Empty reports whether there is nothing to stop.
func (r Report) Empty() bool { return r.numAgent == 0 && r.Sessions() == 0 }

// Scan works out what the cord would reach, without touching anything. An
// instance narrows it to one factory's sub-agents; "" is every factory on the
// machine, which is what the shell cord means by default.
func Scan(root string, scope Scope, instance string) Report {
	procs := factory.Snapshot()
	report := Report{Scope: scope, Instance: instance}

	if scope == All {
		report.Loose = procs.RunningAgents()
		sort.Slice(report.Loose, func(i, j int) bool { return report.Loose[i].PID < report.Loose[j].PID })
		report.numAgent = len(report.Loose)
		return report
	}

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
		if member.Kind == factory.NotFactory || member.Kind == factory.Reception {
			continue // the desk stays up; see the package comment
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

	// Workers first, then gaffers: stop the line from the far end, so nothing
	// is still being dispatched into as it goes down.
	sort.SliceStable(report.Targets, func(i, j int) bool {
		return stopOrder(report.Targets[i].Kind) < stopOrder(report.Targets[j].Kind)
	})
	return report
}

func stopOrder(kind factory.Kind) int {
	if kind == factory.Worker {
		return 0
	}
	return 1 // gaffers last
}

// Stop pulls the cord and reports what it reached.
//
// TERM first, everywhere, then a pause, then the sessions. An agent that is
// mid-write gets its chance to finish before its tmux session disappears.
func Stop(report Report) (agents, sessions int) {
	term := func(list []factory.AgentProc) {
		for _, agent := range list {
			if syscall.Kill(agent.PID, syscall.SIGTERM) == nil {
				agents++
			}
		}
	}

	if report.Scope == All {
		term(report.Loose)
		return agents, 0
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
	if r.Scope == All {
		return fmt.Sprintf("%d agent(s) across the whole machine", r.numAgent)
	}
	switch {
	case r.numAgent == 0:
		return fmt.Sprintf("%d sub-agent(s), nothing running in them", len(r.Targets))
	default:
		return fmt.Sprintf("%d agent(s) in %d sub-agent(s)", r.numAgent, len(r.Targets))
	}
}

// Lines describes each thing the cord would stop, one per line.
func (r Report) Lines() []string {
	if r.Scope == All {
		counts := map[string]int{}
		var order []string
		for _, agent := range r.Loose {
			if counts[agent.Label] == 0 {
				order = append(order, agent.Label)
			}
			counts[agent.Label]++
		}
		out := make([]string, 0, len(order))
		for _, label := range order {
			out = append(out, fmt.Sprintf("%d %s", counts[label], label))
		}
		return out
	}

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
func Run(root string, scope Scope, instance string) error {
	report := Scan(root, scope, instance)
	if report.Empty() {
		fmt.Println("  " + ui.Dim.Render("Nothing running. The line is already stopped."))
		return nil
	}

	fmt.Println()
	fmt.Println("  " + ui.Alarm.Render("🚨 STOP THE LINE"))
	fmt.Println("  " + ui.Header.Render(report.Summary()+":"))
	for _, line := range report.Lines() {
		fmt.Println("    " + line)
	}
	if report.Scope == All {
		fmt.Println("  " + ui.Dim.Render("Machine-wide: this reaches agents the factory never started."))
	} else {
		fmt.Println("  " + ui.Dim.Render("TERM, then the sessions. Reception stays up; ./factory brings the gaffers back."))
	}
	fmt.Print("\n  " + ui.Alarm.Render("Stop the line? [y/N] "))

	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		agents, sessions := Stop(report)
		fmt.Println("  " + ui.Flash.Render(fmt.Sprintf("✓ Line stopped — %s", result(agents, sessions))))
	default:
		fmt.Println("  " + ui.Dim.Render("Cancelled. The line keeps running."))
	}
	return nil
}

// result phrases what a pull actually did.
func result(agents, sessions int) string {
	if sessions == 0 {
		return fmt.Sprintf("%d agent(s) sent TERM", agents)
	}
	return fmt.Sprintf("%d agent(s) sent TERM, %d sub-agent(s) closed", agents, sessions)
}
