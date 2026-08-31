// factory list — what factories exist on this machine.
//
// `init` writes a config and `cleanup` removes one, and both take a name you
// are expected to already know. This is where you learn the name, and enough
// beside it to tell a factory that is running from one that is merely
// configured: what it works on, whether this machine is allowed to run it, what
// is up right now, and when it last finished a beat.
//
// It reads files only — configs, the beat log, the tmux session list. Nothing
// here starts, stops or fixes anything, so it is safe to run at any moment,
// including while a beat is in flight.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hev/factory/internal/tmuxctl"
	"github.com/hev/factory/internal/ui"
	"github.com/hev/factory/pkg/factory"
)

// factoryRow is one line of the table, rendered from files rather than guessed.
type factoryRow struct {
	name     string
	runtime  string
	plans    string
	host     string
	sessions string
	lastBeat string
	atHome   bool
	up       bool
}

// factoryInstances is the list command's view of the configured factories,
// named so the test can build rows without going through the printing.
func factoryInstances(root string) []factory.Instance { return factory.LoadInstances(root) }

func runList(root string, args []string) error {
	for _, arg := range args {
		switch arg {
		case "-h", "--help":
			fmt.Print(listUsage)
			return nil
		default:
			return fmt.Errorf("unknown argument %q — factory list takes none", arg)
		}
	}
	if root == "" {
		return fmt.Errorf("no factory checkout found — run ./factory from the repo once, or set FACTORY_ROOT")
	}

	instances := factoryInstances(root)
	if len(instances) == 0 {
		fmt.Println("  " + ui.Dim.Render("No factory configured on this machine."))
		fmt.Println("  " + ui.Dim.Render("run ./factory once, then /reception in the workspace you want to configure."))
		return nil
	}

	rows := make([]factoryRow, 0, len(instances))
	for _, inst := range instances {
		rows = append(rows, describeInstance(root, inst))
	}
	printFactoryTable(rows)
	return nil
}

// describeInstance reads one factory's config and the state around it.
func describeInstance(root string, inst factory.Instance) factoryRow {
	row := factoryRow{
		name:    inst.Name,
		runtime: inst.Runtime,
		plans:   inst.PlansRepo,
		atHome:  inst.AtHome(),
	}
	if row.plans == "" {
		row.plans = "—"
	} else if branch := inst.Branch(); branch != factory.DefaultPlansBranch {
		// Only when it is not the usual branch. A factory reading something
		// other than main is the case worth seeing without asking, and
		// stamping "@main" on every row would bury it.
		row.plans += "@" + branch
	}

	row.host = inst.HomeHost
	if row.host == "" {
		row.host = "—"
	} else if row.atHome {
		row.host = "this machine"
	}

	// What is actually up, named the one way everything is named.
	var parts []string
	if tmuxctl.HasSession(factory.GafferFor(inst.Name)) {
		parts = append(parts, "gaffer")
	}
	if workers := countWorkers(root, inst.Name); workers > 0 {
		parts = append(parts, fmt.Sprintf("%d worker%s", workers, plural(workers)))
	}
	row.up = len(parts) > 0
	row.sessions = strings.Join(parts, ", ")
	if row.sessions == "" {
		row.sessions = "—"
	}

	// The beat log is the honest record of a completed iteration: the heartbeat
	// is also touched at boot, so it says "beat" about a factory that has never
	// finished one (docs/feedback/2026-08-21-first-factory.md).
	row.lastBeat = "never"
	home, err := os.UserHomeDir()
	if err == nil {
		beats := filepath.Join(home, ".factory", "beats", inst.Name+".jsonl")
		if info, err := os.Stat(beats); err == nil {
			row.lastBeat = ui.Duration(int(time.Since(info.ModTime()).Seconds())) + " ago"
		}
	}
	return row
}

// countWorkers counts this instance's live worker sessions, using the same
// classifier the picker draws with.
func countWorkers(root, instance string) int {
	scope := factory.NewScope(root)
	now := time.Now()
	count := 0
	for _, session := range tmuxctl.ListSessions() {
		member := scope.Classify(session.Name, now)
		if member.Kind == factory.Worker && member.Instance == instance {
			count++
		}
	}
	return count
}

func printFactoryTable(rows []factoryRow) {
	headers := []string{"NAME", "RUNTIME", "PLANS REPO", "HOST", "SESSIONS", "LAST BEAT"}
	widths := make([]int, len(headers))
	for n, header := range headers {
		widths[n] = len(header)
	}
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cell := []string{row.name, row.runtime, row.plans, row.host, row.sessions, row.lastBeat}
		for n, value := range cell {
			if len(value) > widths[n] {
				widths[n] = len(value)
			}
		}
		cells = append(cells, cell)
	}

	fmt.Println()
	var head strings.Builder
	for n, header := range headers {
		head.WriteString(ui.Pad(header, widths[n]) + "  ")
	}
	fmt.Println("  " + ui.Header.Render(strings.TrimRight(head.String(), " ")))

	for n, cell := range cells {
		var line strings.Builder
		line.WriteString(ui.InstanceStyle(cell[0]).Render(ui.Pad(cell[0], widths[0])) + "  ")
		for c := 1; c < len(cell); c++ {
			padded := ui.Pad(cell[c], widths[c])
			switch {
			case c == 4 && rows[n].up:
				padded = ui.Working.Render(padded)
			case c == 3 && !rows[n].atHome:
				padded = ui.Dim.Render(padded)
			}
			line.WriteString(padded + "  ")
		}
		fmt.Println("  " + strings.TrimRight(line.String(), " "))
	}

	// One line of context under the table, and only when it earns its place.
	var away []string
	for _, row := range rows {
		if !row.atHome {
			away = append(away, row.name)
		}
	}
	if len(away) > 0 {
		fmt.Println("  " + ui.Dim.Render(strings.Join(away, ", ")+
			": home is another machine, so ./factory leaves it alone here."))
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

const listUsage = `factory list — the factories configured on this machine

  factory list

One row per factories/<name>.toml: what it works on, whether this machine is
its home_host, which of its sessions are up, and when it last finished a beat.

Read-only. For what is running rather than what is configured, the picker
itself (bare ` + "`factory`" + `) is the live view.
`
