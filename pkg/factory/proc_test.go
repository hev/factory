package factory

import "testing"

func TestCommName(t *testing.T) {
	for input, want := range map[string]string{
		"-zsh":                 "zsh",
		"(zsh)":                "zsh",
		"/opt/homebrew/bin/gh": "gh",
		"claude":               "claude",
		"  node  ":             "node",
		"":                     "",
	} {
		if got := commName(input); got != want {
			t.Errorf("commName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAgentForReadsThePaneShellsChild(t *testing.T) {
	// pane shell 100 has claude under it; pane shell 200 is a bare shell whose
	// only child is a login shell the kernel has parenthesised.
	table := &ProcTable{byPID: map[int]*Proc{}, children: map[int][]*Proc{}}
	add := func(pid, ppid int, comm string) {
		proc := &Proc{PID: pid, PPID: ppid, Comm: comm}
		table.byPID[pid] = proc
		table.children[ppid] = append(table.children[ppid], proc)
	}
	add(100, 1, "-zsh")
	add(101, 100, "2.1.4") // claude renames itself to its version; not matched
	add(102, 100, "claude")
	add(200, 1, "-zsh")
	add(201, 200, "(zsh)")
	add(300, 1, "-zsh")
	add(301, 300, "/usr/bin/rsync")

	for pid, want := range map[int]string{
		100: "claude",
		200: "shell",
		300: "rsync",
		400: "shell", // a pane with no children at all
		0:   "shell",
	} {
		if got := table.AgentFor(pid); got != want {
			t.Errorf("AgentFor(%d) = %q, want %q", pid, got, want)
		}
	}
}

func TestRunningAgentsSkipsTheDesktopApp(t *testing.T) {
	table := &ProcTable{byPID: map[int]*Proc{}, children: map[int][]*Proc{}}
	add := func(pid int, args string) { table.byPID[pid] = &Proc{PID: pid, Args: args} }
	add(1, "/opt/homebrew/bin/claude --continue")
	add(2, "/Applications/Claude.app/Contents/MacOS/Claude --type=renderer")
	add(3, "codex exec")
	add(4, "node /usr/local/bin/openclaw-gateway")
	add(5, "vim claude-notes.md") // names an agent, is not one

	found := map[string]int{}
	for _, agent := range table.RunningAgents() {
		found[agent.Label]++
	}
	if len(found) != 3 || found["Claude Code"] != 1 || found["Codex"] != 1 || found["OpenClaw Gateway"] != 1 {
		t.Errorf("RunningAgents() = %+v, want one each of Claude Code, Codex, OpenClaw Gateway", found)
	}
}
