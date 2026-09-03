package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/hev/factory/pkg/factory"
)

// Reception can be opened in a workspace on a machine that is not the
// factory's home_host — a laptop clone of a repo whose factory runs on a mini
// is the ordinary case, not an edge one. There, the local tmux server has no
// gaffer session and the local ~/.factory is either empty or a stale copy from
// when this machine *was* home. Reporting either as the factory's state is
// worse than saying nothing: "down" is wrong, and a stale beat count is wrong
// while looking current.
//
// So when this is not the home host, ask the home host.

const (
	// A SessionStart hook runs before every session in the workspace. It must
	// never hang, so probes refuse to prompt and give up quickly; an
	// unreachable home host degrades to unknown rather than blocking work.
	sshProbeTimeout = 6 * time.Second
	stateUnknown    = "unknown"
)

// awayHost is the machine to ask, and whether asking is needed at all.
func awayHost(inst factory.Instance) (string, bool) {
	if inst.HomeHost == "" || inst.AtHome() || !safeInstanceName(inst.Name) {
		return "", false
	}
	return inst.HomeHost, true
}

// safeInstanceName gates interpolation into a remote shell command. Names come
// from factories/<name>.toml, so this is belt-and-braces rather than a real
// boundary — but the alternative is quoting rules that have to be right in two
// shells at once.
func safeInstanceName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// sshProbe runs one short read-only command on the home host.
func sshProbe(host, script string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), sshProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=4",
		"-o", "StrictHostKeyChecking=accept-new",
		host, script)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// gafferStateOn mirrors factory.GafferState across ssh. One-shot instances are
// not probed remotely: their liveness is a lock directory that says nothing
// useful about a machine you are not on.
func gafferStateOn(host string, inst factory.Instance) string {
	if inst.Runtime == "one-shot" {
		return factory.GafferState(inst)
	}
	// `ssh host cmd` runs a non-interactive shell that sources neither the
	// login profile nor the interactive rc, so a tmux installed by a package
	// manager is frequently not on PATH there. Report that as unknown: a
	// missing probe and a stopped gaffer are different facts, and collapsing
	// them tells the operator the factory is down when it is running.
	out, ok := sshProbe(host, fmt.Sprintf(
		"command -v tmux >/dev/null 2>&1 || { echo missing; exit 0; }; "+
			"tmux has-session -t '=%s' 2>/dev/null && echo running || echo down",
		factory.GafferFor(inst.Name)))
	if !ok || out == "missing" {
		return stateUnknown
	}
	if out == "running" {
		return "running"
	}
	return "down"
}

// waitingOn reads the last beat on the home host. Deliberately no fallback to
// the local file: a stale local beat reported as current is the failure this
// whole file exists to prevent.
func waitingOn(host, instance string) (int, bool) {
	out, ok := sshProbe(host, fmt.Sprintf(
		`tail -n 1 "$HOME/.factory/beats/%s.jsonl" 2>/dev/null`, instance))
	if !ok || out == "" {
		return 0, false
	}
	var beat struct {
		Waiting int `json:"waiting"`
	}
	if err := json.Unmarshal([]byte(out), &beat); err != nil {
		return 0, false
	}
	return beat.Waiting, true
}

// unreadEventsOn counts the home host's unread event lines, resolving the
// factory root there rather than assuming it matches this machine's.
func unreadEventsOn(host, instance string) int {
	out, ok := sshProbe(host, fmt.Sprintf(
		`root="$(cat "$HOME/.factory/root" 2>/dev/null)"; `+
			`[ -n "$root" ] && "$root/scripts/factory-events.sh" %s --count --reader reception 2>/dev/null`,
		instance))
	if !ok {
		return -1
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d", &n); err != nil {
		return -1
	}
	return n
}
