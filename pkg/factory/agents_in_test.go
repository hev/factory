package factory

import "testing"

func TestAgentsInWalksTheWholeSubtree(t *testing.T) {
	table := &ProcTable{byPID: map[int]*Proc{}, children: map[int][]*Proc{}}
	add := func(pid, ppid int, args string) {
		proc := &Proc{PID: pid, PPID: ppid, Args: args}
		table.byPID[pid] = proc
		table.children[ppid] = append(table.children[ppid], proc)
	}
	// pane shell -> wrapper script -> claude: two levels down, still an agent.
	add(100, 1, "-zsh")
	add(101, 100, "/bin/bash /Users/you/bin/run-loop.sh")
	add(102, 101, "/opt/homebrew/bin/claude --continue")
	add(103, 100, "node /usr/local/bin/openclaw-gateway")
	add(104, 100, "vim notes.md") // not an agent
	// a different pane entirely, which must not be swept in
	add(200, 1, "-zsh")
	add(201, 200, "codex exec")

	under := table.AgentsIn(100)
	if len(under) != 2 {
		t.Fatalf("AgentsIn(100) found %d agents, want 2 (nested claude + gateway): %+v", len(under), under)
	}
	found := map[int]bool{}
	for _, agent := range under {
		found[agent.PID] = true
	}
	if !found[102] {
		t.Error("an agent under a wrapper script must still be found")
	}
	if found[201] {
		t.Error("an agent in another pane must not be swept in")
	}

	if got := table.AgentsIn(0); got != nil {
		t.Errorf("AgentsIn(0) = %+v, want nil", got)
	}
	if got := table.AgentsIn(999); len(got) != 0 {
		t.Errorf("AgentsIn of an unknown pid = %+v, want empty", got)
	}

	// A pane with no shell in between: the pane process is the agent.
	bare := &ProcTable{byPID: map[int]*Proc{}, children: map[int][]*Proc{}}
	bare.byPID[300] = &Proc{PID: 300, PPID: 1, Args: "claude --continue"}
	if got := bare.AgentsIn(300); len(got) != 1 || got[0].PID != 300 {
		t.Errorf("AgentsIn(300) = %+v, want the pane process itself", got)
	}
}
