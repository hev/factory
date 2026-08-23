package factory

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Proc is one process in the machine's table.
type Proc struct {
	PID  int
	PPID int
	Comm string // executable name, as the kernel accounts for it
	Args string // command line, as the process has rewritten it
}

// ProcTable is a snapshot of every process, indexed for parent lookups.
type ProcTable struct {
	byPID    map[int]*Proc
	children map[int][]*Proc
}

// Snapshot reads the process table. Two `ps` calls rather than one because the
// two views disagree on purpose: `comm` is what was executed, `args` is what
// the process now claims to be, and an agent CLI rewrites the latter.
func Snapshot() *ProcTable {
	table := &ProcTable{byPID: map[int]*Proc{}, children: map[int][]*Proc{}}

	for _, line := range run("ps", "-axo", "pid=,ppid=,comm=") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		// comm can hold spaces (an app bundle path), so it is the rest of the
		// line rather than the third field.
		comm := strings.TrimSpace(line[strings.Index(line, fields[1])+len(fields[1]):])
		proc := &Proc{PID: pid, PPID: ppid, Comm: comm}
		table.byPID[pid] = proc
		table.children[ppid] = append(table.children[ppid], proc)
	}

	for _, line := range run("ps", "-axo", "pid=,args=") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		if proc, ok := table.byPID[pid]; ok {
			proc.Args = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
		}
	}
	return table
}

func run(name string, args ...string) []string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n")
}

// knownAgents are the foreground processes worth naming in a row. Anything
// else shows up under its own name, and a bare shell shows as "shell".
var knownAgents = map[string]bool{
	"claude": true, "codex": true, "aider": true, "node": true,
	"python": true, "python3": true, "nvim": true, "vim": true,
	"ssh": true, "lazygit": true, "gh": true, "bun": true, "deno": true,
}

// AgentFor names the foreground process in a pane.
//
// tmux's own pane_current_command is unreliable here: claude rewrites its
// process title to its version number, so the pane reports "2.1.4" as the
// command. The child of the pane's shell is what actually answers the
// question.
func (t *ProcTable) AgentFor(panePID int) string {
	if t == nil || panePID == 0 {
		return "shell"
	}
	kids := t.children[panePID]
	if len(kids) == 0 {
		return "shell"
	}
	for _, kid := range kids {
		if name := commName(kid.Comm); knownAgents[name] {
			return name
		}
	}
	switch name := commName(kids[0].Comm); name {
	case "zsh", "bash", "sh", "fish", "":
		return "shell"
	default:
		return name
	}
}

// commName normalises what ps prints in the comm column: a login shell wears a
// leading dash, and macOS parenthesises the name of a process it cannot read
// properly — a zombie, or one caught mid-exec. Neither is a different program.
func commName(comm string) string {
	comm = strings.TrimSpace(comm)
	if comm == "" {
		return ""
	}
	name := filepath.Base(comm)
	name = strings.TrimPrefix(strings.TrimSuffix(name, ")"), "(")
	return strings.TrimPrefix(name, "-")
}

// AgentProc is a running agent the andon cord can stop.
type AgentProc struct {
	PID   int
	Label string
}

// agentPatterns are the CLIs "stop the line" halts, matched against the
// command line. The claude pattern is deliberately narrow: it takes the CLI
// invoked with a flag and leaves the Claude desktop app running.
var agentPatterns = []struct {
	label string
	re    *regexp.Regexp
}{
	{"Claude Code", regexp.MustCompile(`(^|/)claude\s+(--|-c\b)`)},
	{"Codex", regexp.MustCompile(`(^|/)codex(\s|$)`)},
	{"OpenClaw Gateway", regexp.MustCompile(`openclaw-gateway`)},
}

// agentLabel names a process if it is an agent, and reports whether it is one.
func agentLabel(proc *Proc) (string, bool) {
	if strings.Contains(proc.Args, "Claude.app") {
		return "", false
	}
	for _, pattern := range agentPatterns {
		if pattern.re.MatchString(proc.Args) {
			return pattern.label, true
		}
	}
	return "", false
}

// RunningAgents lists every agent process on this machine, in no order. This is
// the machine-wide reach, which the andon cord uses only when asked for it
// explicitly — see AgentsUnder for the scoped answer.
func (t *ProcTable) RunningAgents() []AgentProc {
	if t == nil {
		return nil
	}
	var out []AgentProc
	for _, proc := range t.byPID {
		if label, ok := agentLabel(proc); ok {
			out = append(out, AgentProc{PID: proc.PID, Label: label})
		}
	}
	return out
}

// AgentsIn lists the agents running in a pane, given the pane's process.
//
// The whole subtree, not the direct children, because an agent is not always
// the pane shell's own child: it can sit under a wrapper script, a `script`
// invocation, or a `/loop` runner. And the pane process itself counts, because
// a session started as `tmux new-session claude` has no shell in between. A
// cord that missed either would report the line stopped while it was still
// moving.
func (t *ProcTable) AgentsIn(pid int) []AgentProc {
	if t == nil || pid == 0 {
		return nil
	}
	var out []AgentProc
	seen := map[int]bool{pid: true}

	if proc, ok := t.byPID[pid]; ok {
		if label, isAgent := agentLabel(proc); isAgent {
			out = append(out, AgentProc{PID: pid, Label: label})
		}
	}

	queue := []int{pid}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, kid := range t.children[parent] {
			if seen[kid.PID] {
				continue // a cycle cannot happen in a process tree, but a
			} // malformed ps parse should not spin forever either
			seen[kid.PID] = true
			if label, ok := agentLabel(kid); ok {
				out = append(out, AgentProc{PID: kid.PID, Label: label})
			}
			queue = append(queue, kid.PID)
		}
	}
	return out
}
