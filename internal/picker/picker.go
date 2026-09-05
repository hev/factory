// Package picker is the factory floor's front door: every gaffer and every
// worker, across every line, on one screen, live, with the one control that
// stops them.
//
// A machine running several factories opens on all of them at once, each line
// a section headed by its own config-and-beat detail, because "what is going
// on" is a question about the machine before it is a question about one
// factory. esc narrows to one line through the chooser when one floor is the
// one that matters.
//
// It lists the factories' sessions and nothing else. The gaffers and the
// workers they dispatched are the floor; the other tmux sessions on the
// machine are somebody's own work and belong to a general session switcher,
// not to this one.
package picker

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hev/factory/internal/auth"
	"github.com/hev/factory/internal/machine"
	"github.com/hev/factory/internal/stopline"
	"github.com/hev/factory/internal/tmuxctl"
	"github.com/hev/factory/internal/ui"
	"github.com/hev/factory/pkg/factory"
)

// refreshInterval is how often the whole floor is re-read. Fast enough that a
// sub-agent's dot flips while you watch it, slow enough that a `ps` call and a
// handful of tmux calls stay invisible.
const refreshInterval = 2 * time.Second

// focusInterval is how often the *selected* pane is re-read. Two seconds is
// the right price for twenty sessions and the wrong one for the single pane
// somebody is looking at: a screen that is meant to show work happening should
// show it happening, and one row costs one tmux call.
//
// So the floor ticks at its own pace and the row under the cursor streams.
const focusInterval = 350 * time.Millisecond

// chrome is the fixed furniture around the list: the header, its blank line,
// the prompt, and room for a flash or a confirm border.
const chrome = 8

// ActionKind is what the picker wants done once it has given the terminal
// back. Attaching replaces this process, so it cannot happen while bubbletea
// still owns the screen.
type ActionKind int

const (
	ActionNone ActionKind = iota
	ActionConnect
	// ActionDesk opens a conversation and hands the terminal to it: a
	// factory's front desk, or the one that configures a new factory. It is
	// separate from ActionConnect because the session may not exist yet, and
	// creating it needs a directory and a command that only the caller should
	// be running — the picker still never starts an agent from inside the TUI.
	ActionDesk
	// actionBack returns to the factory chooser on a machine that has more
	// than one. It never reaches the caller.
	actionBack
	// actionAuth opens the logins screen and comes back to the same floor. It
	// never reaches the caller either: nothing on that screen changes the
	// machine, so there is nothing for the caller to perform.
	actionAuth
)

// Action is the picker's one result.
type Action struct {
	Kind     ActionKind
	Name     string // the session to attach to, or to create
	Instance string
	// Dir is where an ActionDesk session opens. Empty for every other kind.
	Dir string
}

type mode int

const (
	modeBrowse mode = iota
	modeConfirmKill
	modeConfirmStop
	// modeCompose is writing a line to the gaffer about the highlighted
	// worker. It is the one thing on this screen that leaves the machine
	// changed without stopping anything.
	modeCompose
)

// focusState is the live read of the pane under the cursor, kept apart from
// the snapshot because it arrives on a different clock.
type focusState struct {
	name  string
	lines []string
}

// linesFor returns the live capture when it belongs to this row, and nothing
// when the cursor has moved since it was taken. Showing one agent's pane under
// another agent's name is the one failure this whole mechanism could produce,
// so it is checked rather than assumed.
func (f focusState) linesFor(name string) []string {
	if f.name != name {
		return nil
	}
	return f.lines
}

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

	detail  bool       // the panel under the list is open
	focus   focusState // the live read of the pane under the cursor
	compose string     // modeCompose: the line being written to the gaffer
}

type refreshMsg snapshot
type tickMsg struct{}
type flashMsg string

// focusTickMsg asks for another read of the selected pane; focusMsg carries
// one back.
type focusTickMsg struct{}
type focusMsg focusState

// Run opens the picker and blocks until the user picks something or leaves.
// The action it returns is performed by the caller, after the terminal is the
// caller's again.
//
// A machine with more than one factory opens on all of them — the whole floor
// is the answer to the question that opened the screen — and `esc` goes to the
// chooser to narrow to one line, or back out to everything. The screens are
// one loop.
func Run(root string) (Action, error) {
	instances := factory.LoadInstances(root)
	instance := ""
	switch len(instances) {
	case 0: // nothing configured yet: the floor says so and points at /reception
	case 1:
		instance = instances[0].Name
	default:
		instance = allLines
	}

	for {
		action, err := runFloor(root, instance, len(instances) > 1)
		if err != nil {
			return Action{}, err
		}
		switch action.Kind {
		case actionAuth:
			// Back to the same floor afterwards. It is a panel somebody opened
			// mid-thought, not a place to be left standing.
			if err := showAuth(); err != nil {
				return Action{}, err
			}
			continue
		case actionBack:
			chosen, err := chooseFactory(root, instances)
			if err != nil || chosen == "" {
				return Action{}, err
			}
			instance = chosen
		default:
			return action, nil
		}
	}
}

func runFloor(root, instance string, canBack bool) (Action, error) {
	summaries.start()
	// The box's own numbers, on their own slow clock. Started here rather than
	// in Run so a floor opened straight from the chooser is sampling too, and
	// idempotent so the round trip does not start a second sampler.
	machine.Start()
	// And the credentials the agents run on, on a slower clock still. The
	// header only ever carries the count; the screen behind ^a carries the rest.
	auth.Start()
	m := model{
		root: root, instance: instance, canBack: canBack,
		height: 24, width: defaultWidth,
		// Open. The detail is what this screen is for, and a panel somebody
		// has to know about before they see it is a panel most people never
		// see. ^d closes it for anyone who wants the bare list back.
		detail: true,
	}
	m.shot = collect(root, instance, nil)
	m.refilter()
	final, err := tea.NewProgram(&m, tea.WithAltScreen()).Run()
	if err != nil {
		return Action{}, err
	}
	return final.(*model).action, nil
}

// Rows prints the floor once and exits — the debugging view, and the way to
// check what the scope rule is including without opening a TUI. It is the same
// all-lines floor the TUI opens on: every factory, sectioned, because the
// question it answers is usually "which of these is picking up that session?"
func Rows(root string) string {
	instances := factory.LoadInstances(root)
	instance := ""
	switch len(instances) {
	case 1:
		instance = instances[0].Name
	default:
		if len(instances) > 1 {
			instance = allLines
		}
	}

	var b strings.Builder
	rows := collect(root, instance, nil).rows
	plan := planColumns(rows, defaultWidth)
	for _, row := range rows {
		b.WriteString(row.render(defaultWidth, plan))
		b.WriteString("\n")
	}
	return b.String()
}

func (m *model) Init() tea.Cmd { return tea.Batch(tick(), focusTick()) }

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func focusTick() tea.Cmd {
	return tea.Tick(focusInterval, func(time.Time) tea.Msg { return focusTickMsg{} })
}

// readFocus captures the selected pane, off the refresh path and off the
// keyboard's. Nothing waits for it: a capture that is slow shows up late, and
// the row keeps whatever it last had.
func (m *model) readFocus() tea.Cmd {
	row := m.selectedRow()
	if row == nil || row.Agent.paneID == "" {
		return nil
	}
	name, pane := row.Name, row.Agent.paneID
	return func() tea.Msg {
		return focusMsg{name: name, lines: tmuxctl.CapturePane(pane, captureLines)}
	}
}

// applyFocus lets the live capture move the row it came from, not just the
// panel below it. The dot and the pane's own words are what somebody watches,
// and there is no reason for them to wait for the next floor refresh when a
// fresher read of that exact pane is already in hand.
//
// A label the model wrote is left alone: it describes a pane state rather than
// a frame, and replacing it with the raw last line every third of a second
// would flicker between two different kinds of answer.
func (m *model) applyFocus() {
	for i := range m.shot.rows {
		row := &m.shot.rows[i]
		if row.Kind != KindAgent || row.Name != m.focus.name {
			continue
		}
		lines := m.focus.lines
		if len(lines) == 0 {
			return
		}
		row.Agent.Tail = lines
		row.Agent.Working = running(lines) || row.Agent.Working
		if !row.Agent.Labelled {
			row.Agent.Doing = paneSummary(lines)
		}
		return
	}
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

	case focusTickMsg:
		return m, tea.Batch(m.readFocus(), focusTick())

	case focusMsg:
		m.focus = focusState(msg)
		m.applyFocus()
		return m, nil

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
	if m.mode == modeCompose {
		return m.composeKey(msg)
	}
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
		return m, m.readFocus()

	case "down", "ctrl+n", "tab":
		m.move(1)
		return m, m.readFocus()

	case "ctrl+x":
		if row := m.selectedRow(); row != nil && row.Kind == KindAgent {
			m.mode = modeConfirmKill
		}
		return m, nil

	case "ctrl+r":
		return m, m.reload()

	case "ctrl+a":
		// The logins. Not a row on this screen — see authview.go — but a
		// keystroke from it, because the moment you want it is the moment a
		// worker on this floor has stopped producing anything.
		m.action = Action{Kind: actionAuth}
		return m, tea.Quit

	case "ctrl+d", "right", "left":
		// The panel is the answer to "and what is that one doing", so it opens
		// on the row somebody is already looking at rather than on a screen of
		// its own. Arrows because that is where an expanding row lives on
		// every other list; ^d because arrows are three keys away from home.
		m.detail = !m.detail
		return m, m.readFocus()

	case "ctrl+g":
		if row := m.selectedRow(); row != nil && row.Kind == KindAgent {
			if m.gafferInstance(*row) == "" {
				m.flash = "no factory configured, so there is no gaffer to tell"
				return m, nil
			}
			m.mode = modeCompose
			m.compose = openingLine(*row)
		}
		return m, nil

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
	case KindAgent:
		m.action = Action{Kind: ActionConnect, Name: row.Name}
		return m, tea.Quit
	case KindReception, KindNewLine:
		m.action = Action{
			Kind:     ActionDesk,
			Name:     row.Name,
			Instance: row.Door.Instance,
			Dir:      row.Door.Dir,
		}
		return m, tea.Quit
	case KindStopLine:
		m.mode = modeConfirmStop
	}
	return m, nil
}

// openingLine is what the compose bar starts with. When the model has already
// said what is wrong, that sentence is the message — the common case becomes
// ^g ↵, and the operator edits only when they know something it does not.
func openingLine(row Row) string {
	if row.Agent.Health.Attention() {
		return row.Agent.Doing
	}
	return ""
}

// composeKey drives the one line being written to the gaffer. It is a plain
// text field on purpose: this is a sentence, not a form.
func (m *model) composeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode, m.compose = modeBrowse, ""
		return m, nil

	case "enter":
		row := m.selectedRow()
		m.mode = modeBrowse
		text := strings.TrimSpace(m.compose)
		m.compose = ""
		if row == nil || text == "" {
			return m, nil
		}
		return m, m.send(*row, text)

	case "backspace":
		if m.compose != "" {
			runes := []rune(m.compose)
			m.compose = string(runes[:len(runes)-1])
		}
		return m, nil

	case "ctrl+u":
		m.compose = ""
		return m, nil
	}

	if msg.Type == tea.KeyRunes && !msg.Alt {
		m.compose += string(msg.Runes)
	} else if msg.Type == tea.KeySpace {
		m.compose += " "
	}
	return m, nil
}

// gafferInstance is which gaffer ^g on this row talks to: the floor's own
// factory, or on the all-lines floor, the factory the row belongs to. That is
// what lets one screen carry every line without the compose bar ever sending a
// worker's trouble to somebody else's gaffer.
func (m *model) gafferInstance(row Row) string {
	if m.instance == allLines {
		return row.Agent.Instance
	}
	return m.instance
}

// send hands the gaffer the operator's line about one worker, with enough
// around it that the gaffer does not have to go and look the worker up. What
// the operator typed goes first: it is the only part of this the gaffer could
// not have worked out for itself.
func (m *model) send(row Row, text string) tea.Cmd {
	a := row.Agent
	body := text + "\n\n" +
		"session: " + row.Name + "\n" +
		"where:   " + a.whereLine() + "\n" +
		"what:    " + a.whatLine() + "\n" +
		"state:   " + a.sinceLine(time.Now())
	if a.Health.Attention() {
		body += "\nreading: " + a.Health.String() + " — " + a.Doing
	}
	body += "\n\nSeen by the operator on the picker. Nothing has been stopped."

	root, instance, name := m.root, m.gafferInstance(row), row.Name
	return tea.Sequence(
		func() tea.Msg {
			if err := factory.GafferMsg(root, instance, body); err != nil {
				return flashMsg("could not tell gaffer-" + instance + ": " + err.Error())
			}
			return flashMsg("told gaffer-" + instance + " about " + name + " — it picks it up on its next beat")
		},
		m.reload(),
	)
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
				return flashMsg(fmt.Sprintf("line stopped — %d agent(s) sent TERM, %d gaffer(s) closed",
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
	width := m.width - 2
	plan := planColumns(m.shot.rows, width)

	// The panel is sized first, because what is left over is what the list
	// gets. Sizing the list first and then discovering the panel does not fit
	// is how a screen ends up scrolling.
	panel := m.panelLines(width)

	b.WriteString("  " + m.header() + "\n\n")

	start, end := m.window(len(panel))
	if start > 0 {
		b.WriteString("  " + ui.Dim.Render(fmt.Sprintf("↑ %d more", start)) + "\n")
	}
	for i := start; i < end; i++ {
		row := m.shot.rows[m.visible[i]]
		pointer := "  "
		line := row.render(width, plan)
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

	for _, line := range panel {
		b.WriteString(" " + line + "\n")
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
	case modeCompose:
		b.WriteString("  " + ui.Dim.Render(m.composeHint()) + "\n")
		b.WriteString("  " + ui.Accent.Render("⚑ ") + m.compose + ui.Dim.Render("▏") + "\n")
	default:
		b.WriteString("  " + ui.Accent.Render("❯ ") + m.filter + ui.Dim.Render("▏") + "\n")
		if m.flash != "" {
			b.WriteString("  " + ui.Flash.Render(m.flash) + "\n")
		}
	}
	return b.String()
}

// panelLines is the detail for the row under the cursor, sized to what the
// terminal can spare.
//
// The transcript is the part that gives: a panel showing one line of an
// agent's own words is still worth more than no panel, and a terminal too
// short even for that gets the list it came for instead.
func (m *model) panelLines(width int) []string {
	if !m.detail {
		return nil
	}
	row := m.selectedRow()
	if row == nil {
		return nil
	}
	live := m.focus.linesFor(row.Name)
	for tail := detailTail; tail >= detailMinTail; tail-- {
		lines := row.detail(width, tail, live)
		if len(lines) == 0 {
			return nil
		}
		if m.height-chrome-len(lines) >= 3 {
			return lines
		}
	}
	return nil
}

// composeHint names who is about to be told what, and when they will act.
// "next beat" is the part worth saying out loud: this is not an interrupt, and
// somebody who thinks it is will stand there watching a row that does not
// change.
func (m *model) composeHint() string {
	name := "the gaffer"
	row := m.selectedRow()
	if row != nil {
		if inst := m.gafferInstance(*row); inst != "" {
			name = "gaffer-" + inst
		}
	}
	// Not "about" the gaffer itself. Telling it that it is looping is a fair
	// thing to want, and "tell gaffer-acme about gaffer-acme" is not how
	// anybody would say it.
	about := ""
	if row != nil && row.Name != name {
		about = " about " + row.Name
	}
	return "⚑ tell " + name + about + " — it reads this on its next beat   ·   ↵ send   ·   esc cancel"
}

// header names the factory this screen is for, because that is the one thing
// the rows below it no longer say: they are all its.
//
// It also carries the count of rows that want somebody, and what the box
// itself is doing. A floor big enough to scroll is a floor where the one red
// row is off screen, and a screen that only shows an alarm when you have
// already scrolled to it is not an alarm.
func (m *model) header() string {
	// Assembled in display order and then thinned, right to left, until it
	// fits. The keys go first: they are the same words every time this screen
	// opens, and everything after them was measured this second. The floor's
	// name and the count of what needs a person are never dropped — between
	// them they are the whole reason the header exists.
	alert := m.alertLine()
	box := machine.Read()
	keys := ui.Header.Render(m.keys())
	for _, attempt := range [][]string{
		{m.floorLabel(), box.Line(), keys, alert},
		// The box gives up its gigabytes and its uptime before the keys strip
		// gives up existing: at 150 columns that is the difference between a
		// screen that still says what ^a does and one that does not.
		{m.floorLabel(), box.Compact(), keys, alert},
		{m.floorLabel(), box.Line(), alert},
		{m.floorLabel(), box.Compact(), alert},
		{m.floorLabel(), alert},
		{m.instanceLabel(), alert},
	} {
		line := joinHeader(attempt)
		if m.width <= 0 || ui.Width(line) <= m.width-2 {
			return line
		}
	}
	return m.instanceLabel()
}

// keys is the hint strip. It is static, which is exactly why it is the first
// thing a narrow terminal gives up.
func (m *model) keys() string {
	keys := "↵ attach  ^d details  ^g tell gaffer  ^x stop one  ^a logins  ·  type to filter"
	if m.canBack {
		keys += "  ·  esc switches factory"
	}
	return keys
}

// instanceLabel is which floor this is, and nothing else.
func (m *model) instanceLabel() string {
	switch {
	case m.instance == allLines:
		return ui.Normal.Render("all lines")
	case m.instance != "":
		return ui.InstanceStyle(m.instance).Render(m.instance)
	}
	return ""
}

// floorLabel is that, plus the counts on the all-lines floor: the total is the
// one number that should not scroll off a screen whose sections do.
func (m *model) floorLabel() string {
	label := m.instanceLabel()
	if m.instance == allLines {
		return label + "  " + ui.Dim.Render(m.floorCount())
	}
	return label
}

// joinHeader spaces the segments that survived and drops the empty ones — a
// floor with nothing in trouble should not be three spaces wider than one that
// has not been measured yet.
func joinHeader(segments []string) string {
	var kept []string
	for _, s := range segments {
		if s != "" {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, "   ")
}

// floorCount is the whole floor in one phrase: how many gaffers and workers
// are up across every line. It sits in the all-lines header because that
// screen's sections scroll, and the total is the one number that should not.
func (m *model) floorCount() string {
	var gaffers, workers int
	for _, row := range m.shot.rows {
		if row.Kind != KindAgent {
			continue
		}
		if row.Agent.Gaffer {
			gaffers++
		} else {
			workers++
		}
	}
	return fmt.Sprintf("%d gaffer%s · %d worker%s",
		gaffers, plural(gaffers), workers, plural(workers))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// alertLine counts what the model thinks needs a person. Trouble is drawn
// first and in red because it is the one somebody should walk towards.
func (m *model) alertLine() string {
	var trouble, waiting int
	for _, row := range m.shot.rows {
		if row.Kind != KindAgent {
			continue
		}
		switch row.Agent.Health {
		case HealthTrouble:
			trouble++
		case HealthWaiting:
			waiting++
		}
	}

	var parts []string
	if trouble > 0 {
		parts = append(parts, ui.Trouble.Render(fmt.Sprintf("! %d in trouble", trouble)))
	}
	if waiting > 0 {
		parts = append(parts, ui.Waiting.Render(fmt.Sprintf("? %d waiting on you", waiting)))
	}
	if login := loginAlert(); login != "" {
		parts = append(parts, login)
	}
	return strings.Join(parts, "   ")
}

// loginAlert is the credentials, in the smallest thing worth saying about
// them: how many want attention, and the key that shows which.
//
// It is on the floor's header because the floor is the screen people have
// open. A token that lapses tomorrow is not urgent today and is nearly free to
// renew today, which is exactly the kind of news that only ever gets acted on
// if it is already in front of somebody.
func loginAlert() string {
	creds := auth.Latest()
	if len(creds) == 0 {
		return "" // not read yet: say nothing rather than say all clear
	}
	// Only the state decides, never the raw date. A refreshable token whose
	// access half expires tonight is graded ok precisely because it renews
	// itself, and a header that flagged it anyway would be contradicting the
	// screen it is pointing at.
	var wanted []auth.Credential
	for _, c := range creds {
		if c.Attention() {
			wanted = append(wanted, c)
		}
	}
	switch len(wanted) {
	case 0:
		return ""
	case 1:
		return ui.Trouble.Render("⚿ " + wanted[0].Name + " " + wanted[0].State.String() + " (^a)")
	default:
		return ui.Trouble.Render(fmt.Sprintf("⚿ %d logins need you (^a)", len(wanted)))
	}
}

// stopPrompt spells out what the cord reaches, which is exactly the sub-agent
// rows above it and nothing else on the machine.
func (m *model) stopPrompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "🚨 stop the line — %s?\n", m.shot.cord.Summary())
	for _, line := range m.shot.cord.Lines() {
		fmt.Fprintf(&b, "   %s\n", line)
	}
	b.WriteString("   TERM first, then the sessions.   [y/N]")
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
//
// panel is how many lines the detail below the list has already claimed.
func (m *model) window(panel int) (start, end int) {
	capacity := m.height - chrome - panel
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
