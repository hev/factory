package fleet

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Harnesses a session can run on.
var Harnesses = []string{"claude", "codex"}

// turnCommand is one non-interactive turn: the first when resume is empty,
// a follow-up to the harness's own session otherwise. The input always
// arrives on stdin, never in argv, so a brief can be any length and hold any
// quote.
//
// Both harnesses run with every approval off. That is the product, and the
// README's first warning.
func turnCommand(m Meta, resume string) []string {
	switch m.Harness {
	case "codex":
		cmd := []string{"codex", "exec"}
		if resume != "" {
			cmd = append(cmd, "resume")
		}
		cmd = append(cmd, "--json", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check")
		if m.Model != "" {
			cmd = append(cmd, "-m", m.Model)
		}
		if resume != "" {
			cmd = append(cmd, resume)
		}
		return append(cmd, "-")
	default:
		// -p is also what keeps the workspace trust dialog away: an
		// interactive claude in a directory it has never seen waits forever on
		// a question nobody is there to answer.
		cmd := []string{"claude", "-p", "--output-format", "stream-json", "--verbose",
			"--permission-mode", "bypassPermissions"}
		if m.Model != "" {
			cmd = append(cmd, "--model", m.Model)
		}
		if resume != "" {
			cmd = append(cmd, "--resume", resume)
		}
		return cmd
	}
}

// interactiveCommand is the TUI a person gets when they attach.
func interactiveCommand(m Meta, resume string) []string {
	switch m.Harness {
	case "codex":
		return []string{"codex", "resume", resume, "--dangerously-bypass-approvals-and-sandbox", "-C", m.Worktree}
	default:
		return []string{"claude", "--resume", resume, "--permission-mode", "bypassPermissions"}
	}
}

// event is the union of the fields either harness's stream carries that the
// factory reads. Everything else passes through to the log untouched.
type event struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`

	// claude
	SessionID string          `json:"session_id"`
	Model     string          `json:"model"`
	Message   json.RawMessage `json:"message"`
	Result    string          `json:"result"`
	IsError   bool            `json:"is_error"`
	CostUSD   float64         `json:"total_cost_usd"`

	// codex
	ThreadID string     `json:"thread_id"`
	Item     *codexItem `json:"item"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`

	// the runner's own marker between turns
	Input string `json:"input"`
	Turn  int    `json:"turn"`
}

type codexItem struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Command string `json:"command"`
	Status  string `json:"status"`
	Exit    *int   `json:"exit_code"`
	Server  string `json:"server"`
	Tool    string `json:"tool"`
	Query   string `json:"query"`
	Changes []struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	} `json:"changes"`
}

type claudeMessage struct {
	Content []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Name    string          `json:"name"`
		Input   json.RawMessage `json:"input"`
		Content json.RawMessage `json:"content"`
		IsError bool            `json:"is_error"`
	} `json:"content"`
}

// observe folds one stream line into the state: the harness's session id the
// first time it appears, and each turn's outcome as it ends.
func observe(line []byte, s *State) {
	var e event
	if json.Unmarshal(line, &e) != nil {
		return
	}
	switch {
	case e.Type == "system" && e.Subtype == "init" && e.SessionID != "":
		s.HarnessSession = e.SessionID
	case e.Type == "result":
		s.Result = clip(e.Result, 4000)
		s.CostUSD += e.CostUSD
		if e.IsError {
			s.Exit = 1
		}
	case e.Type == "thread.started" && e.ThreadID != "":
		s.HarnessSession = e.ThreadID
	case e.Type == "item.completed" && e.Item != nil && e.Item.Type == "agent_message":
		s.Result = clip(e.Item.Text, 4000)
	case e.Type == "turn.failed" && e.Error != nil:
		s.Result = clip(e.Error.Message, 4000)
		s.Exit = 1
	}
}

// Render turns the log into what a person reads: what the agent said, each
// tool it reached for on one line, errors, and where each turn began and
// ended. Tool output is left out; it is in the log, and in the trace.
func Render(lines [][]byte) []string {
	var out []string
	add := func(prefix, text string) {
		for i, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			if i == 0 {
				out = append(out, prefix+l)
			} else {
				out = append(out, strings.Repeat(" ", len([]rune(prefix)))+l)
			}
		}
	}
	for _, line := range lines {
		var e event
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		switch e.Type {
		case "factory":
			if e.Turn <= 1 {
				add("▶ ", "turn 1: the brief")
			} else {
				add("▶ ", fmt.Sprintf("turn %d: %s", e.Turn, clip(e.Input, 300)))
			}
		case "assistant":
			var m claudeMessage
			if json.Unmarshal(e.Message, &m) != nil {
				continue
			}
			for _, c := range m.Content {
				switch c.Type {
				case "text":
					if strings.TrimSpace(c.Text) != "" {
						add("", c.Text)
					}
				case "tool_use":
					add("  → ", c.Name+" "+toolSummary(c.Name, c.Input))
				}
			}
		case "user":
			var m claudeMessage
			if json.Unmarshal(e.Message, &m) != nil {
				continue
			}
			for _, c := range m.Content {
				if c.Type == "tool_result" && c.IsError {
					add("  ✗ ", clip(firstLine(textOf(c.Content)), 200))
				}
			}
		case "result":
			tail := ""
			if e.CostUSD > 0 {
				tail = fmt.Sprintf(" ($%.2f)", e.CostUSD)
			}
			if e.IsError {
				add("■ ", "turn failed"+tail+": "+clip(e.Result, 500))
			} else {
				add("■ ", "turn done"+tail)
			}
		case "item.completed":
			if e.Item == nil {
				continue
			}
			it := e.Item
			switch it.Type {
			case "agent_message":
				add("", it.Text)
			case "command_execution":
				mark := "  → "
				if it.Exit != nil && *it.Exit != 0 {
					mark = "  ✗ "
				}
				add(mark, "$ "+clip(firstLine(it.Command), 200))
			case "file_change":
				for _, c := range it.Changes {
					add("  → ", c.Kind+" "+c.Path)
				}
			case "mcp_tool_call":
				add("  → ", it.Server+"."+it.Tool)
			case "web_search":
				add("  → ", "search "+it.Query)
			}
		case "turn.completed":
			add("■ ", "turn done")
		case "turn.failed":
			msg := ""
			if e.Error != nil {
				msg = e.Error.Message
			}
			add("■ ", "turn failed: "+clip(msg, 500))
		case "error":
			if e.Error != nil {
				add("✗ ", e.Error.Message)
			}
		}
	}
	return out
}

func toolSummary(name string, input json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	for _, key := range []string{"command", "file_path", "path", "pattern", "url", "query", "description", "prompt"} {
		if v, ok := in[key].(string); ok && v != "" {
			return clip(firstLine(v), 160)
		}
	}
	return clip(string(input), 160)
}

// textOf reads a tool_result's content, which is either a string or a list
// of text blocks.
func textOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &blocks)
	var parts []string
	for _, b := range blocks {
		parts = append(parts, b.Text)
	}
	return strings.Join(parts, "\n")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
