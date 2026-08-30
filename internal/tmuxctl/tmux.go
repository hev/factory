// Package tmuxctl is the thin layer over the tmux CLI: everything the picker
// reads about live sessions, and the two things it does to them (attach,
// kill).
package tmuxctl

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Session is one live tmux session.
type Session struct {
	Name     string
	Attached bool
	Activity time.Time
}

// Pane is the active pane of a session — the one whose directory, foreground
// process, and last output the picker reports.
type Pane struct {
	ID   string // tmux's own pane id (%12), the only unambiguous way to name one
	PID  int
	Path string
}

const sep = "\x1f" // unit separator: safe inside session names and paths

func tmux(args ...string) *exec.Cmd { return exec.Command("tmux", args...) }

func lines(cmd *exec.Cmd) []string {
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// ListSessions returns every live session, most recently active first — so the
// session you just left is at the top of the list when you come back.
func ListSessions() []Session {
	format := strings.Join([]string{"#{session_name}", "#{session_attached}", "#{session_activity}"}, sep)
	var out []Session
	for _, line := range lines(tmux("list-sessions", "-F", format)) {
		parts := strings.Split(line, sep)
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		session := Session{Name: parts[0], Attached: parts[1] != "0" && parts[1] != ""}
		if epoch, err := strconv.ParseInt(parts[2], 10, 64); err == nil && epoch > 0 {
			session.Activity = time.Unix(epoch, 0)
		}
		out = append(out, session)
	}
	sortByActivity(out)
	return out
}

func sortByActivity(sessions []Session) {
	for i := 1; i < len(sessions); i++ {
		for j := i; j > 0 && sessions[j].Activity.After(sessions[j-1].Activity); j-- {
			sessions[j], sessions[j-1] = sessions[j-1], sessions[j]
		}
	}
}

// PaneRef is one pane and the session it belongs to.
type PaneRef struct {
	Session string
	PID     int
}

// AllPanes lists every pane, not just the active one per session. Reading a
// row only needs the active pane; stopping a session has to reach an agent in
// any window of it.
func AllPanes() []PaneRef {
	format := strings.Join([]string{"#{session_name}", "#{pane_pid}"}, sep)
	var out []PaneRef
	for _, line := range lines(tmux("list-panes", "-a", "-F", format)) {
		parts := strings.Split(line, sep)
		if len(parts) < 2 {
			continue
		}
		pid, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		out = append(out, PaneRef{Session: parts[0], PID: pid})
	}
	return out
}

// ActivePanes maps each session to its active pane, in one tmux call.
func ActivePanes() map[string]Pane {
	format := strings.Join([]string{
		"#{session_name}", "#{pane_active}", "#{pane_id}", "#{pane_pid}", "#{pane_current_path}",
	}, sep)
	out := map[string]Pane{}
	for _, line := range lines(tmux("list-panes", "-a", "-F", format)) {
		parts := strings.Split(line, sep)
		if len(parts) < 5 || parts[1] != "1" {
			continue
		}
		pid, _ := strconv.Atoi(parts[3])
		out[parts[0]] = Pane{ID: parts[2], PID: pid, Path: parts[4]}
	}
	return out
}

// CapturePane returns the last n lines of a pane, with no escape sequences in
// them. This is how the picker reads what an agent is doing: the pane is the
// only place a running agent writes its state down, and reading it costs one
// tmux call per sub-agent per refresh.
//
// The pane is named by its tmux id (%12) rather than by its session, because
// `-t` on capture-pane wants a pane and resolves a bare name by matching.
func CapturePane(paneID string, n int) []string {
	if paneID == "" {
		return nil
	}
	if n <= 0 {
		n = 20
	}
	return lines(tmux("capture-pane", "-p", "-t", paneID, "-S", "-"+strconv.Itoa(n)))
}

// HasSession reports whether a session exists.
func HasSession(name string) bool {
	return tmux("has-session", "-t", "="+name).Run() == nil
}

// InsideTmux reports whether the picker is itself running in a tmux pane,
// which decides between switching the client and attaching a new one.
func InsideTmux() bool { return os.Getenv("TMUX") != "" }

// CurrentSessionAttached reports whether somebody can currently see the tmux
// session containing this process. A picker run directly in a terminal is
// visible by definition. The summarizer uses this to avoid model calls for a
// picker left running in a session the operator has detached from.
func CurrentSessionAttached() bool {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return true
	}
	out, err := tmux("display-message", "-p", "-t", pane, "#{session_attached}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != "0" && strings.TrimSpace(string(out)) != ""
}

// Connect hands the terminal to a session and does not return: switch-client
// when we are already inside tmux, otherwise a fresh attach that replaces this
// process, so detaching lands back wherever the picker was launched from.
func Connect(name string) error {
	if InsideTmux() {
		return tmux("switch-client", "-t", "="+name).Run()
	}
	return execTmux("attach", "-t", "="+name)
}

// KillSession ends a session and everything running in it.
func KillSession(name string) error {
	return tmux("kill-session", "-t", "="+name).Run()
}

// execTmux replaces this process with tmux. The picker is usually the last
// thing in a login loop, and replacing it keeps that loop honest: when tmux
// exits, the shell decides what happens next, not a picker still on the stack.
func execTmux(args ...string) error {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	return syscall.Exec(path, append([]string{"tmux"}, args...), os.Environ())
}
