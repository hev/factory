package picker

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hev/factory/internal/auth"
	"github.com/hev/factory/internal/ui"
)

// The logins screen: whether the credentials the agents run on are still good,
// and when they stop being good.
//
// It is a screen of its own rather than rows on the floor because it answers a
// different question about a different subject. The floor is what the agents
// are doing; this is what they are standing on. Mixing them would put six rows
// that change once a month in the middle of twenty that change every two
// seconds, and the floor would be the thing that suffered.
//
// What earns it a key on the floor's header is the failure mode. A lapsed
// credential does not stop an agent — it lets one run for an hour and produce
// nothing, and every one of those hours was avoidable by looking here first.
type authModel struct {
	creds   []auth.Credential
	cursor  int
	width   int
	height  int
	probing bool
	note    string
}

type authProbeMsg []auth.Credential

// showAuth opens the logins screen and blocks until it is dismissed. It
// returns nothing: this screen reads, and the fixing is done at a shell.
func showAuth() error {
	m := &authModel{creds: auth.Latest(), width: defaultWidth, height: 24}
	if len(m.creds) == 0 {
		m.creds = auth.Refresh()
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *authModel) Init() tea.Cmd { return nil }

func (m *authModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height, m.width = msg.Height, msg.Width
		return m, nil

	case authProbeMsg:
		m.creds, m.probing = []auth.Credential(msg), false
		auth.Store(m.creds)
		m.note = "asked the services themselves"
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			return m, tea.Quit
		case "up", "ctrl+p", "shift+tab", "k":
			m.move(-1)
		case "down", "ctrl+n", "tab", "j":
			m.move(1)
		case "ctrl+r", "r":
			// The live pass. It is behind a key because it goes to the
			// network and one of its calls can hang; the note says it is
			// running so nobody presses it twice.
			if !m.probing {
				m.probing = true
				m.note = "asking github and 1password…"
				return m, probeCmd(m.creds)
			}
		}
	}
	return m, nil
}

func probeCmd(creds []auth.Credential) tea.Cmd {
	return func() tea.Msg { return authProbeMsg(auth.Probe(creds)) }
}

func (m *authModel) move(delta int) {
	if len(m.creds) == 0 {
		return
	}
	m.cursor = (m.cursor + delta + len(m.creds)) % len(m.creds)
}

// The columns. Same discipline as the floor: fixed by hand so the eye can
// travel down one, with the free text last.
const (
	authName  = 12
	authWhat  = 26
	authState = 14
	authWhen  = 15
)

func (m *authModel) View() string {
	var b strings.Builder
	b.WriteString("  " + m.header() + "\n\n")

	for i, c := range m.creds {
		pointer := "  "
		line := authLine(c, m.width-4)
		if i == m.cursor {
			pointer = ui.Accent.Render("▸") + " "
			line = ui.Selected.Render(line)
		}
		b.WriteString(pointer + line + "\n")
	}

	// The selected row's source, spelled out. It is the answer to "says who?",
	// which is the first thing anybody asks a screen like this, and it is the
	// path they need anyway to go and fix it.
	if c, ok := m.selected(); ok && c.Source != "" {
		b.WriteString("\n  " + ui.Dim.Render("read from "+c.Source) + "\n")
	}
	if m.note != "" {
		b.WriteString("\n  " + ui.Flash.Render(m.note) + "\n")
	}
	return b.String()
}

func (m *authModel) selected() (auth.Credential, bool) {
	if m.cursor < 0 || m.cursor >= len(m.creds) {
		return auth.Credential{}, false
	}
	return m.creds[m.cursor], true
}

func (m *authModel) header() string {
	head := ui.Normal.Render("logins") + "  " + ui.Dim.Render(authSummary(m.creds))
	return head + "   " + ui.Header.Render("^r check live   ·   esc back")
}

// authSummary counts the reading, so the state of the machine's credentials is
// one phrase rather than six rows to add up.
func authSummary(creds []auth.Credential) string {
	counts := map[auth.State]int{}
	for _, c := range creds {
		counts[c.State]++
	}
	var parts []string
	for _, state := range []auth.State{auth.StateOK, auth.StateExpiring, auth.StateExpired, auth.StateMissing, auth.StateUnknown} {
		if n := counts[state]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, state))
		}
	}
	if len(parts) == 0 {
		return "nothing to check"
	}
	return strings.Join(parts, " · ")
}

func authLine(c auth.Credential, width int) string {
	line := authStyle(c.State).Render(authMark(c.State)) + " " +
		ui.Normal.Render(ui.Pad(c.Name, authName)) + "  " +
		ui.Dim.Render(ui.Pad(c.What, authWhat)) + "  " +
		authStyle(c.State).Render(ui.Pad(c.State.String(), authState)) + "  " +
		authStyle(c.State).Render(ui.Pad(expiryPhrase(c), authWhen))

	room := width - (2 + authName + 2 + authWhat + 2 + authState + 2 + authWhen + 2)
	if room >= 12 && c.Detail != "" {
		line += "  " + ui.Dim.Render(ui.Pad(c.Detail, room))
	}
	return strings.TrimRight(line, " ")
}

// authMark is the one cell that answers "does this need me?", ranked the way
// the floor's is: fix it, renew it, or nothing.
func authMark(s auth.State) string {
	switch s {
	case auth.StateExpired, auth.StateMissing:
		return "!"
	case auth.StateExpiring:
		return "?"
	case auth.StateOK:
		return "●"
	}
	return "·"
}

func authStyle(s auth.State) lipgloss.Style {
	switch s {
	case auth.StateOK:
		return ui.Working
	case auth.StateExpiring:
		return ui.Waiting
	case auth.StateExpired, auth.StateMissing:
		return ui.Trouble
	}
	return ui.Dim
}

// expiryPhrase is the date, in the direction it matters. A credential with no
// recorded expiry gets a dash rather than a guess — the row's state already
// says the reading is uncertain, and a fabricated date would be the one thing
// on this screen worth not trusting.
func expiryPhrase(c auth.Credential) string {
	if c.Expires.IsZero() {
		return "—"
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
	return phrase
}
