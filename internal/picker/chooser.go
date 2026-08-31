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

// A screen belongs to one factory. Two factories on a machine are two separate
// concerns — different repos, different plans, different people waiting on
// them — and a list that mixes their sub-agents together makes you read the
// instance column before you can read anything else. So a machine running more
// than one asks which before it shows anything.
//
// One factory skips this screen entirely: there is nothing to choose.
type choice struct {
	Instance factory.Instance
	Gaffer   bool
	Workers  int
}

type chooser struct {
	choices []choice
	cursor  int
	picked  string
}

// chooseFactory asks which factory the floor should be. It returns "" when the
// operator leaves without picking one.
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
		m.cursor = (m.cursor - 1 + len(m.choices)) % len(m.choices)
	case "down", "ctrl+n", "tab":
		m.cursor = (m.cursor + 1) % len(m.choices)
	case "enter":
		if m.cursor < len(m.choices) {
			m.picked = m.choices[m.cursor].Instance.Name
		}
		return m, tea.Quit
	}
	// A digit picks a factory outright, which is faster than arrowing to it on
	// a machine that runs the same three every day.
	if typed := key.String(); len(typed) == 1 {
		if n := int(typed[0]) - '1'; n >= 0 && n < len(m.choices) {
			m.picked = m.choices[n].Instance.Name
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *chooser) View() string {
	var b strings.Builder
	b.WriteString("  " + ui.Header.Render("which factory?   ↵ open   ·   1-9 jumps   ·   esc leaves") + "\n\n")

	for i, c := range m.choices {
		pointer := "  "
		if i == m.cursor {
			pointer = ui.Accent.Render("▸") + " "
		}
		line := fmt.Sprintf("%s %s %s %s",
			ui.Dim.Render(fmt.Sprintf("%d", i+1)),
			ui.InstanceStyle(c.Instance.Name).Render(ui.Pad(c.Instance.Name, 12)),
			ui.Dim.Render(ui.Pad(c.Instance.PlansRepo, 28)),
			ui.Dim.Render(c.status()))
		if i == m.cursor {
			line = ui.Selected.Render(line)
		}
		b.WriteString(pointer + line + "\n")
	}
	return b.String()
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
