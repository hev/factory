package picker

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hev/factory/internal/tmuxctl"
	"github.com/hev/factory/internal/ui"
	"github.com/hev/factory/pkg/factory"
)

// The chooser is how a floor narrows. A machine running several factories
// opens on all of them at once; this screen is where esc lands, offering one
// line at a time for when two factories' sub-agents mixed together is more to
// read than the moment needs — different repos, different plans, different
// people waiting on them. The all-lines row at the top is the way back out.
//
// One factory skips this screen entirely: there is nothing to choose.
type choice struct {
	Instance factory.Instance
	Gaffer   bool
	Workers  int
}

type chooser struct {
	choices []choice
	cursor  int // 0 is the all-lines row; factories start at 1
	picked  string
}

// rows is how many lines the chooser offers: every factory, plus all of them.
func (m *chooser) rows() int { return len(m.choices) + 1 }

// chooseFactory asks which floor to open: one factory's, or every line at
// once (it returns allLines). "" means the operator left without picking.
func chooseFactory(root string, instances []factory.Instance) (string, error) {
	m := &chooser{choices: survey(root, instances)}
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return "", err
	}
	return final.(*chooser).picked, nil
}

// survey counts what is up for each configured factory, so the choice is made
// against the machine rather than against the config.
func survey(root string, instances []factory.Instance) []choice {
	scope := factory.NewScope(root)
	now := time.Now()
	live := map[string]*choice{}

	out := make([]choice, len(instances))
	for i, inst := range instances {
		out[i] = choice{Instance: inst}
		live[inst.Name] = &out[i]
	}

	for _, s := range tmuxctl.ListSessions() {
		member := scope.Classify(s.Name, now)
		row, ok := live[member.Instance]
		if !ok {
			continue
		}
		switch member.Kind {
		case factory.Gaffer:
			row.Gaffer = true
		case factory.Worker:
			row.Workers++
		}
	}
	return out
}

func (m *chooser) Init() tea.Cmd { return nil }

func (m *chooser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok || len(m.choices) == 0 {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c", "esc", "q":
		return m, tea.Quit
	case "up", "ctrl+p", "shift+tab":
		m.cursor = (m.cursor - 1 + m.rows()) % m.rows()
	case "down", "ctrl+n", "tab":
		m.cursor = (m.cursor + 1) % m.rows()
	case "enter":
		if m.cursor == 0 {
			m.picked = allLines
		} else if m.cursor-1 < len(m.choices) {
			m.picked = m.choices[m.cursor-1].Instance.Name
		}
		return m, tea.Quit
	}
	// A digit picks a floor outright, which is faster than arrowing to it on a
	// machine that runs the same three every day. 0 matches the row it is
	// drawn on: everything.
	if typed := key.String(); len(typed) == 1 {
		if typed == "0" || typed == "a" {
			m.picked = allLines
			return m, tea.Quit
		}
		if n := int(typed[0]) - '1'; n >= 0 && n < len(m.choices) {
			m.picked = m.choices[n].Instance.Name
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *chooser) View() string {
	var b strings.Builder
	b.WriteString("  " + ui.Header.Render("which floor?   ↵ open   ·   0 everything, 1-9 one line   ·   esc leaves") + "\n\n")

	all := fmt.Sprintf("%s %s %s %s",
		ui.Dim.Render("0"),
		ui.Normal.Render(ui.Pad("all lines", 12)),
		ui.Dim.Render(ui.Pad("every factory, sectioned", 28)),
		ui.Dim.Render(m.allStatus()))
	pointer := "  "
	if m.cursor == 0 {
		pointer = ui.Accent.Render("▸") + " "
		all = ui.Selected.Render(all)
	}
	b.WriteString(pointer + all + "\n")

	for i, c := range m.choices {
		pointer := "  "
		if i+1 == m.cursor {
			pointer = ui.Accent.Render("▸") + " "
		}
		line := fmt.Sprintf("%s %s %s %s",
			ui.Dim.Render(fmt.Sprintf("%d", i+1)),
			ui.InstanceStyle(c.Instance.Name).Render(ui.Pad(c.Instance.Name, 12)),
			ui.Dim.Render(ui.Pad(c.Instance.PlansRepo, 28)),
			ui.Dim.Render(c.status()))
		if i+1 == m.cursor {
			line = ui.Selected.Render(line)
		}
		b.WriteString(pointer + line + "\n")
	}
	return b.String()
}

// allStatus sums the floor, so the everything row answers the same question
// the per-factory rows do: is anything running, and how much.
func (m *chooser) allStatus() string {
	var gaffers, workers int
	for _, c := range m.choices {
		if c.Gaffer {
			gaffers++
		}
		workers += c.Workers
	}
	if gaffers == 0 && workers == 0 {
		return "not running"
	}
	return fmt.Sprintf("%d gaffer(s) · %d sub-agent(s)", gaffers, workers)
}

// status is the one phrase that says whether this factory is running: the desk,
// the loop, and how many sub-agents are out.
func (c choice) status() string {
	if !c.Gaffer && c.Workers == 0 {
		return "not running"
	}
	parts := make([]string, 0, 3)
	if c.Gaffer {
		parts = append(parts, "gaffer")
	}
	if c.Workers > 0 {
		parts = append(parts, fmt.Sprintf("%d sub-agent(s)", c.Workers))
	}
	return strings.Join(parts, " · ")
}
