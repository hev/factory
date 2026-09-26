package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeStream(t *testing.T) {
	stream := []string{
		`{"type":"factory","turn":1,"input":"the brief"}`,
		`{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-opus-5-5"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Looking at the tests."},{"type":"tool_use","name":"Bash","input":{"command":"go test ./...\nsecond line"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","is_error":true,"content":"exit 1\nFAIL"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"Opened #12.","total_cost_usd":0.42}`,
		`{"type":"factory","turn":2,"input":"also fix the lint"}`,
	}
	var st State
	var lines [][]byte
	for _, l := range stream {
		observe([]byte(l), &st)
		lines = append(lines, []byte(l))
	}
	if st.HarnessSession != "sess-1" || st.Result != "Opened #12." || st.CostUSD != 0.42 || st.Exit != 0 {
		t.Fatalf("state = %+v", st)
	}
	got := strings.Join(Render(lines), "\n")
	for _, want := range []string{
		"▶ turn 1: the brief",
		"Looking at the tests.",
		"  → Bash go test ./... …",
		"  ✗ exit 1 …",
		"■ turn done ($0.42)",
		"▶ turn 2: also fix the lint",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q in:\n%s", want, got)
		}
	}
}

func TestClaudeErrorResult(t *testing.T) {
	var st State
	observe([]byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"boom"}`), &st)
	if st.Exit != 1 || st.Result != "boom" {
		t.Fatalf("state = %+v", st)
	}
}

func TestCodexStream(t *testing.T) {
	stream := []string{
		`{"type":"thread.started","thread_id":"th-9"}`,
		`{"type":"item.completed","item":{"id":"1","type":"command_execution","command":"make test","exit_code":2,"status":"failed"}}`,
		`{"type":"item.completed","item":{"id":"2","type":"file_change","changes":[{"path":"a.go","kind":"update"}]}}`,
		`{"type":"item.completed","item":{"id":"3","type":"agent_message","text":"Done."}}`,
		`{"type":"turn.completed","usage":{"input_tokens":1}}`,
	}
	var st State
	var lines [][]byte
	for _, l := range stream {
		observe([]byte(l), &st)
		lines = append(lines, []byte(l))
	}
	if st.HarnessSession != "th-9" || st.Result != "Done." {
		t.Fatalf("state = %+v", st)
	}
	got := strings.Join(Render(lines), "\n")
	for _, want := range []string{"  ✗ $ make test", "  → update a.go", "Done.", "■ turn done"} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q in:\n%s", want, got)
		}
	}
}

func TestTurnCommand(t *testing.T) {
	m := Meta{Harness: "claude", Model: "m"}
	if got := strings.Join(turnCommand(m, "s1"), " "); got != "claude -p --output-format stream-json --verbose --permission-mode bypassPermissions --model m --resume s1" {
		t.Errorf("claude resume = %s", got)
	}
	m.Harness = "codex"
	if got := strings.Join(turnCommand(m, ""), " "); got != "codex exec --json --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check -m m -" {
		t.Errorf("codex first = %s", got)
	}
	if got := strings.Join(turnCommand(m, "th"), " "); got != "codex exec resume --json --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check -m m th -" {
		t.Errorf("codex resume = %s", got)
	}
}

func TestPlace(t *testing.T) {
	info := func(live, cores int, load float64, mem int, used float64) *Info {
		return &Info{Cores: cores, Live: live, Load: load, MemFreePct: mem,
			Harnesses: []string{"claude"}, Usage: map[string]*Usage{"claude": {UsedPct: used}}}
	}
	local := Host{Name: "local"}
	mini := Host{Name: "mini", SSH: "mini"}

	h, err := Place([]Candidate{{Host: local, Info: info(0, 10, 1, 50, 10)}, {Host: mini, Info: info(0, 12, 1, 90, 10)}}, "claude")
	if err != nil || h.Name != "mini" {
		t.Fatalf("always-on host first: got %v %v", h.Name, err)
	}
	h, err = Place([]Candidate{{Host: local, Info: info(0, 10, 1, 50, 10)}, {Host: mini, Info: info(6, 12, 1, 90, 10)}}, "claude")
	if err != nil || h.Name != "local" {
		t.Fatalf("full mini spills to local: got %v %v", h.Name, err)
	}
	_, err = Place([]Candidate{{Host: local, Info: info(0, 10, 1, 50, 97)}, {Host: mini, Err: os.ErrDeadlineExceeded}}, "claude")
	if err == nil || !strings.Contains(err.Error(), "mini: unreachable") || !strings.Contains(err.Error(), "97% of its week") {
		t.Fatalf("refuses and says why: %v", err)
	}
	_, err = Place([]Candidate{{Host: local, Info: info(0, 10, 1, 50, 10)}}, "codex")
	if err == nil {
		t.Fatal("placed a codex session on a host without codex")
	}
}

func TestInboxAndRunnerLock(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	id := "abc123"
	if err := os.MkdirAll(sessionDir(id), 0o755); err != nil {
		t.Fatal(err)
	}
	enqueue(id, "one")
	time.Sleep(time.Millisecond)
	enqueue(id, "two")
	files := inbox(id)
	if len(files) != 2 {
		t.Fatalf("inbox = %v", files)
	}
	first, _ := os.ReadFile(files[0])
	if string(first) != "one" {
		t.Fatalf("oldest first, got %q", first)
	}
	if runnerAlive(id) {
		t.Fatal("no runner yet")
	}
	unlock, err := flock(filepath.Join(sessionDir(id), "runner.lock"), false)
	if err != nil {
		t.Fatal(err)
	}
	if !runnerAlive(id) {
		t.Fatal("held lock reads as a live runner")
	}
	unlock()
	if got, err := Resolve("abc"); err != nil || got != id {
		t.Fatalf("Resolve prefix = %q %v", got, err)
	}
	if _, err := Resolve("zzz"); err != ErrNoSession {
		t.Fatalf("Resolve miss = %v", err)
	}
}

func TestFactoryBriefSeam(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	bin := t.TempDir()
	m := Meta{ID: "seam01", Repo: "hev/lyr", Branch: "factory/seam01", Harness: "codex", Host: "echo",
		Worktree: t.TempDir(), Task: "fix the flaky port test", Base: "main"}
	os.MkdirAll(sessionDir(m.ID), 0o755)

	t.Setenv("PATH", bin)
	if got := extraBrief(m); got != "" {
		t.Fatalf("no factory-brief on PATH still added %q", got)
	}
	script := "#!/bin/sh\nread task\necho \"On the board about $FACTORY_REPO for: $task ($FACTORY_HARNESS)\"\n"
	os.WriteFile(filepath.Join(bin, "factory-brief"), []byte(script), 0o755)
	t.Setenv("PATH", bin+":/bin:/usr/bin")
	extra := extraBrief(m)
	if want := "On the board about hev/lyr for: fix the flaky port test (codex)"; !strings.Contains(extra, want) {
		t.Fatalf("extra = %q", extra)
	}
	b := brief(m, "hevbot", extra)
	if i, j := strings.Index(b, "On the board"), strings.Index(b, "Task:\nfix the flaky"); i < 0 || j < i {
		t.Fatalf("brief context must come before the task:\n%s", b)
	}

	os.WriteFile(filepath.Join(bin, "factory-brief"), []byte("#!/bin/sh\necho partial\nexit 3\n"), 0o755)
	if got := extraBrief(m); got != "" {
		t.Fatalf("a failing factory-brief added %q", got)
	}
}
