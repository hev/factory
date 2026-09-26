// Package tmuxctl is the thin layer over the tmux CLI: the sessions a host
// is running, what their panes show, and ending one. A factory session's
// runner and an attached TUI both live in tmux, so this is how the factory
// sees them.
package tmuxctl

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Session is one live tmux session.
type Session struct {
	Name     string
	Attached bool
	Activity time.Time
}

// Pane is a session's active pane.
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

// ListSessions returns every live session, most recently active first.
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
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Activity.After(out[j-1].Activity); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
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
// them: what an attached TUI is showing. The pane is named by its tmux id
// (%12) rather than by its session, because `-t` on capture-pane wants a pane
// and resolves a bare name by matching.
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

// KillSession ends a session and everything running in it.
func KillSession(name string) error {
	return tmux("kill-session", "-t", "="+name).Run()
}
