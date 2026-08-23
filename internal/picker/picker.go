// Package picker is one factory's front door: every sub-agent that factory is
// running, on one screen, live, with the one control that stops them.
//
// It lists that factory's sessions and nothing else. Its reception, its gaffer,
// and the workers the gaffer dispatched are the floor; the other tmux sessions
// on the machine are somebody's own work — including the other factories' —
// and belong to a general session switcher, not to this one.
package picker

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hev/factory/pkg/factory"
	"github.com/hev/factory/internal/stopline"
	"github.com/hev/factory/internal/tmuxctl"
	"github.com/hev/factory/internal/ui"
)

// refreshInterval is how often the floor is re-read. Fast enough that a
// sub-agent's dot flips while you watch it, slow enough that a `ps` call and a
// handful of tmux calls stay invisible.
const refreshInterval = 2 * time.Second

// ActionKind is what the picker wants done once it has given the terminal
// back. Attaching replaces this process, so it cannot happen while bubbletea
// still owns the screen.
type ActionKind int

const (
	ActionNone ActionKind = iota
	ActionConnect
	// ActionDesk is ↵ on a front desk that is not on duty: put it back up,
	// then attach to it. Booting a desk takes a claude start-up and prints as
	// it goes, so it belongs out here with the terminal rather than behind a
	// TUI that would have to hold still for it.
	ActionDesk
	// actionBack returns to the factory chooser on a machine that has more
	// than one. It never reaches the caller.
	actionBack
)

// Action is the picker's one result.
type Action struct {
	Kind     ActionKind
	Name     string // the session to attach to
	Instance string // ActionDesk: whose desk it is, "" for the bootstrap one
}

type mode int

const (
	modeBrowse mode = iota
	modeConfirmKill
	modeConfirmStop
)

type model struct {
	root     string
	instance string // the one factory this screen is for, "" before init
	canBack  bool   // more than one factory is configured

	shot    snapshot
	visible []int // indices into shot.rows that survive the filter
	cursor  int   // index into visible
	filter  string
	mode    mode
	flash   string
	height  int
	width   int
	action  Action
}

type refreshMsg snapshot
type tickMsg struct{}
type flashMsg string

// Run opens the picker and blocks until the user picks something or leaves.
// The action it returns is performed by the caller, after the terminal is the
// caller's again.
//
// A machine with more than one factory chooses one first, and `esc` on the
// floor comes back here rather than quitting — the two screens are one loop.
func Run(root string) (Action, error) {
	for {
		instances := factory.LoadInstances(root)
		instance := ""
		switch len(instances) {
		case 0: // nothing configured yet: the bootstrap desk is the whole floor
		case 1:
			instance = instances[0].Name
		default:
			chosen, err := chooseFactory(root, instances)
			if err != nil || chosen == "" {
				return Action{}, err
			}
			instance = chosen
		}

		action, err := runFloor(root, instance, len(instances) > 1)
		if err != nil || action.Kind != actionBack {
			return action, err
		}
	}
}

func runFloor(root, instance string, canBack bool) (Action, error) {
	summaries.start()
	m := model{root: root, instance: instance, canBack: canBack, height: 24, width: defaultWidth}
	m.shot = collect(root, instance, nil)
	m.refilter()
	final, err := tea.NewProgram(&m, tea.WithAltScreen()).Run()
	if err != nil {
		return Action{}, err
	}
	return final.(*model).action, nil
}

// Rows prints the floors once and exits — the debugging view, and the way to
// check what the scope rule is including without opening a TUI. The TUI shows
// one factory at a time; this dumps every one, named, because the question it
// answers is usually "which of these is picking up that session?"
func Rows(root string) string {
	var b strings.Builder
	names := instanceNames(root)
	for _, inst := range names {
		if len(names) > 1 {
			b.WriteString(ui.Dim.Render("── "+inst+" ──") + "\n")
		}
		for _, row := range collect(root, inst, nil).rows {
			b.WriteString(row.render(defaultWidth))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// instanceNames is every configured factory, or one unnamed floor on a machine
// that has none yet.
func instanceNames(root string) []string {
	instances := factory.LoadInstances(root)
	if len(instances) == 0 {
		return []string{""}
	}
	names := make([]string, 0, len(instances))
	for _, inst := range instances {
		names = append(names, inst.Name)
	}
	return names
}

func (m *model) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) reload() tea.Cmd {
	root, instance, panes := m.root, m.instance, m.shot.panes
	return func() tea.Msg { return refreshMsg(collect(root, instance, panes)) }
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height, m.width = msg.Height, msg.Width
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.reload(), tick())

	case refreshMsg:
		// Hold the cursor on the row it was on rather than on the line number
		// it was on: a refresh that reorders the floor should not move the
		// selection out from under a keypress.
		selected := m.selectedRow()
		m.shot = snapshot(msg)
		m.refilter()
		m.restore(selected)
		return m, nil

	case flashMsg:
		m.flash = string(msg)
		return m, nil

	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeBrowse {
		return m.confirmKey(msg)
	}

	m.flash = "" // a result is news until the next keypress, then it is clutter

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit

	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.refilter()
			return m, nil
		}
		if m.canBack {
			m.action = Action{Kind: actionBack}
		}
		return m, tea.Quit

	case "up", "ctrl+p", "shift+tab":
		m.move(-1)
		return m, nil

	case "down", "ctrl+n", "tab":
		m.move(1)
		return m, nil

	case "ctrl+x":
		if row := m.selectedRow(); row != nil && row.Kind == KindAgent {
			m.mode = modeConfirmKill
		}
		return m, nil

	case "ctrl+r":
		return m, m.reload()

	case "backspace":
		if m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
			m.refilter()
		}
		return m, nil

	case "ctrl+u":
		m.filter = ""
		m.refilter()
		return m, nil

	case "enter":
		return m.activate()
	}

	if msg.Type == tea.KeyRunes && !msg.Alt {
		m.filter += string(msg.Runes)
		m.refilter()
	} else if msg.Type == tea.KeySpace {
		m.filter += " "
		m.refilter()
	}
	return m, nil
}

// activate is what ↵ does, which depends only on the row it lands on.
func (m *model) activate() (tea.Model, tea.Cmd) {
	row := m.selectedRow()
	if row == nil {
		return m, nil
	}
	switch row.Kind {
	case KindReception:
		m.action = Action{Kind: ActionConnect, Name: row.Name, Instance: m.instance}
		if !row.Up {
			m.action.Kind = ActionDesk
		}
		return m, tea.Quit
	case KindAgent:
		m.action = Action{Kind: ActionConnect, Name: row.Name}
		return m, tea.Quit
	case KindStopLine:
		m.mode = modeConfirmStop
	}
	return m, nil
}

func (m *model) confirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		pending := m.mode
		m.mode = modeBrowse
		return m, m.commit(pending)
	default:
		m.mode = modeBrowse
		return m, nil
	}
}

// commit performs a confirmed action and refreshes. Both of these change the
// floor, so the screen the user is returned to is the new one.
func (m *model) commit(pending mode) tea.Cmd {
	switch pending {
	case modeConfirmKill:
		row := m.selectedRow()
		if row == nil || row.Kind != KindAgent {
			return nil
		}
		name := row.Name
		return tea.Sequence(
			func() tea.Msg {
				if err := tmuxctl.KillSession(name); err != nil {
					return flashMsg("could not stop " + name + ": " + err.Error())
				}
				return flashMsg("stopped " + name)
			},
			m.reload(),
		)

	case modeConfirmStop:
		cord := m.shot.cord
		return tea.Sequence(
			func() tea.Msg {
				agents, sessions := stopline.Stop(cord)
				return flashMsg(fmt.Sprintf("line stopped — %d agent(s) sent TERM, %d sub-agent(s) closed",
					agents, sessions))
			},
			m.reload(),
		)
	}
	return nil
}

// ── selection ────────────────────────────────────────────────

func (m *model) refilter() {
	m.visible = m.visible[:0]
	for i, row := range m.shot.rows {
		// Separators and the empty-floor note are scaffolding: they belong to
		// the unfiltered list and disappear the moment a query narrows it.
		if !row.selectable() {
			if m.filter == "" {
				m.visible = append(m.visible, i)
			}
			continue
		}
		if matches(row.Search, m.filter) {
			m.visible = append(m.visible, i)
		}
	}
	m.clampCursor()
}

func (m *model) clampCursor() {
	if len(m.visible) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if !m.shot.rows[m.visible[m.cursor]].selectable() {
		m.move(1)
	}
}

// move steps the cursor over selectable rows, wrapping at both ends the way
// the list has always wrapped.
func (m *model) move(delta int) {
	if len(m.visible) == 0 {
		return
	}
	for range m.visible {
		m.cursor = (m.cursor + delta + len(m.visible)) % len(m.visible)
		if m.shot.rows[m.visible[m.cursor]].selectable() {
			return
		}
	}
}

func (m *model) selectedRow() *Row {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	row := m.shot.rows[m.visible[m.cursor]]
	if !row.selectable() {
		return nil
	}
	return &row
}

// restore puts the cursor back on the same row after a refresh, falling back
// to the same position when that row is gone.
func (m *model) restore(prev *Row) {
	if prev == nil {
		return
	}
	for i, idx := range m.visible {
		row := m.shot.rows[idx]
		if row.Kind == prev.Kind && row.Name == prev.Name {
			m.cursor = i
			return
		}
	}
	m.clampCursor()
}

// ── view ─────────────────────────────────────────────────────

func (m *model) View() string {
	var b strings.Builder

	b.WriteString("  " + m.header() + "\n\n")

	start, end := m.window()
	if start > 0 {
		b.WriteString("  " + ui.Dim.Render(fmt.Sprintf("↑ %d more", start)) + "\n")
	}
	for i := start; i < end; i++ {
		row := m.shot.rows[m.visible[i]]
		pointer := "  "
		line := row.render(m.width - 2)
		if i == m.cursor && row.selectable() {
			pointer = ui.Accent.Render("▸") + " "
			line = ui.Selected.Render(line)
		}
		b.WriteString(pointer + line + "\n")
	}
	if hidden := len(m.visible) - end; hidden > 0 {
		b.WriteString("  " + ui.Dim.Render(fmt.Sprintf("↓ %d more", hidden)) + "\n")
	}
	if len(m.visible) == 0 {
		b.WriteString("  " + ui.Dim.Render("no match") + "\n")
	}

	b.WriteString("\n")
	switch m.mode {
	case modeConfirmKill:
		if row := m.selectedRow(); row != nil {
			b.WriteString(indent(ui.Confirm.Render(
				"stop '"+row.Name+"'?  the agent running in it is terminated   [y/N]")) + "\n")
		}
	case modeConfirmStop:
		b.WriteString(indent(ui.Confirm.Render(m.stopPrompt())) + "\n")
	default:
		b.WriteString("  " + ui.Accent.Render("❯ ") + m.filter + ui.Dim.Render("▏") + "\n")
		if m.flash != "" {
			b.WriteString("  " + ui.Flash.Render(m.flash) + "\n")
		}
	}
	return b.String()
}

// header names the factory this screen is for, because that is the one thing
// the rows below it no longer say: they are all its.
func (m *model) header() string {
	keys := "↵ attach   ^x stop one   ·   type to filter"
	if m.canBack {
		keys += "   ·   esc switches factory"
	}
	if m.instance == "" {
		return ui.Header.Render(keys)
	}
	return ui.InstanceStyle(m.instance).Render(m.instance) + "   " + ui.Header.Render(keys)
}

// stopPrompt spells out what the cord reaches, which is exactly the sub-agent
// rows above it. Reception is not among them, and neither is anything else on
// the machine.
func (m *model) stopPrompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "🚨 stop the line — %s?\n", m.shot.cord.Summary())
	for _, line := range m.shot.cord.Lines() {
		fmt.Fprintf(&b, "   %s\n", line)
	}
	b.WriteString("   TERM first, then the sessions. Reception stays up.   [y/N]")
	return b.String()
}

// indent shifts a whole block right, not just its first line — a bordered
// confirm is several lines and they all have to start in the same column.
func indent(block string) string {
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}

// window is the slice of rows that fits on screen, scrolled to keep the cursor
// in view. A factory with twenty sub-agents out should still be navigable on a
// laptop; a factory with three never sees this run.
func (m *model) window() (start, end int) {
	// Header, blank line, prompt, and room for a flash or a confirm border.
	const chrome = 8
	capacity := m.height - chrome
	if capacity < 3 {
		capacity = 3
	}
	if len(m.visible) <= capacity {
		return 0, len(m.visible)
	}
	start = m.cursor - capacity/2
	if start < 0 {
		start = 0
	}
	if start > len(m.visible)-capacity {
		start = len(m.visible) - capacity
	}
	return start, start + capacity
}
