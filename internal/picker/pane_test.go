package picker

import "testing"

// A real Claude Code pane, mid-beat: transcript, then the input box, then the
// status bar. The last thing the agent said about itself is above all of it.
const claudePane = `  Listed 1 directory, ran 4 shell commands

⏺ Two commands tripped on zsh globbing of ===. Rerunning quoted.

  Ran 2 shell commands

⏺ All quiet on the queue, inbox, and worker floor. One active plan exists
  (plans/active/intake-inbox.md) — I need to compute what's left of it before
  closing the beat.

  Read 1 file, ran 3 shell commands

✻ Brewed for 1m 17s

────────────────────────────────────────────────────────────────────────────────
❯
────────────────────────────────────────────────────────────────────────────────
    travelswithcharlie  main ≡  ?6~6  Fable 5  ▰▰▰▰▱
  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents`

func TestPaneSummary(t *testing.T) {
	tests := []struct {
		name string
		pane string
		want string
	}{
		{
			name: "prefers the agent's own status line over the box below it",
			pane: claudePane,
			want: "✻ Brewed for 1m 17s",
		},
		{
			name: "strips the keyboard hint from a spinner",
			pane: "⏺ Bash(npm test)\n\n✻ Herding… (12s · ↑ 1.2k tokens · esc to interrupt)\n\n" +
				"────────────────────────\n❯ \n────────────────────────\n  ⏵⏵ bypass permissions on",
			want: "✻ Herding… (12s · ↑ 1.2k tokens)",
		},
		{
			name: "falls back to the last line when nothing is marked",
			pane: "make: *** [test] Error 1\n\n────────────────────\n❯ \n────────────────────",
			want: "make: *** [test] Error 1",
		},
		{
			name: "a pane with no input box is all transcript",
			pane: "$ git status\nOn branch main\nnothing to commit\n$ ",
			want: "$",
		},
		{
			name: "collapses the runs of spaces a TUI pads with",
			pane: "⏺ Read(plans/active/intake-inbox.md)     +12 lines\n\n───────────\n❯ \n───────────",
			want: "⏺ Read(plans/active/intake-inbox.md) +12 lines",
		},
		{name: "an empty pane says nothing", pane: "", want: ""},
		{name: "a blank pane says nothing", pane: "\n\n   \n", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := paneSummary(splitLines(tc.pane))
			if got != tc.want {
				t.Errorf("paneSummary() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunning(t *testing.T) {
	tests := []struct {
		name string
		pane string
		want bool
	}{
		{"a spinner with no keyboard hint still means running", claudePane, true},
		{"the keyboard hint means running", "⏺ Bash(go test ./...)\n  ⎿ running… esc to interrupt\n", true},
		{
			name: "a finished turn is not running",
			pane: "⏺ Done. Two files changed.\n\n──────────\n❯ \n──────────\n  ⏵⏵ bypass permissions on",
			want: false,
		},
		{"a shell is not running", "$ git status\nnothing to commit\n$ ", false},
		{"an empty pane is not running", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := running(splitLines(tc.pane)); got != tc.want {
				t.Errorf("running() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPaneDigestIgnoresTrailingBlanks(t *testing.T) {
	a := paneDigest([]string{"⏺ Read(main.go)", "", "  "})
	b := paneDigest([]string{"⏺ Read(main.go)"})
	if a != b {
		t.Errorf("paneDigest() moved on whitespace alone: %q vs %q", a, b)
	}
	if paneDigest([]string{"⏺ Read(main.go)"}) == paneDigest([]string{"⏺ Read(other.go)"}) {
		t.Error("paneDigest() did not move on changed content")
	}
}

func TestAboveInputBoxKeepsPanesWithoutOne(t *testing.T) {
	lines := []string{"one", "two", "three"}
	if got := aboveInputBox(lines); len(got) != 3 {
		t.Errorf("aboveInputBox() dropped lines from a box-less pane: %q", got)
	}
}

func TestIsRule(t *testing.T) {
	for _, line := range []string{"────────────────", "  ╭──────────────╮  "} {
		if !isRule(line) {
			t.Errorf("isRule(%q) = false, want true", line)
		}
	}
	for _, line := range []string{"", "───", "⏺ Bash(ls)", "── sub-agents ──"} {
		if isRule(line) {
			t.Errorf("isRule(%q) = true, want false", line)
		}
	}
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
