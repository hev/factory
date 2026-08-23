package picker

import (
	"hash/fnv"
	"strconv"
	"strings"
	"unicode"
)

// captureLines is how far back into a pane the picker reads. Enough to clear
// the input box at the bottom and still land on the last thing the agent said.
const captureLines = 40

// markers are the glyphs a coding agent puts at the head of the line when it
// says what it is doing: an action, a thought, a tool result. A line carrying
// one is worth more than the prose above it, so the summary prefers them.
const markers = "⏺✻✽✳✶✷·⎿⧉"

// markerLookback is how far above the last line to hunt for a marker before
// settling for whatever the final line happens to be.
const markerLookback = 12

// interruptHints are what an agent prints while it is holding the keyboard:
// the way out of a run in progress. Nothing prints them when it is waiting for
// you, which makes them an instant answer to "is this thing working?" that
// costs no history.
var interruptHints = []string{"esc to interrupt", "ctrl+c to cancel", "ctrl+c to stop"}

// spinners are the glyphs an agent animates while it is thinking. They are
// drawn only during a run — a finished turn leaves ⏺ lines behind and no
// spinner — so a transcript ending on one is a transcript still being written.
// An unattached pane often renders the spinner without the keyboard hint, which
// is exactly the case the factory is full of.
const spinners = "✻✽✳✶✷⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

// running reports whether the pane is showing an agent mid-run.
func running(lines []string) bool {
	for i := len(lines) - 1; i >= 0 && i > len(lines)-markerLookback; i-- {
		lower := strings.ToLower(lines[i])
		for _, hint := range interruptHints {
			if strings.Contains(lower, hint) {
				return true
			}
		}
	}
	transcript := trimTrailing(aboveInputBox(lines))
	for i := len(transcript) - 1; i >= 0; i-- {
		line := strings.TrimSpace(transcript[i])
		if line == "" {
			continue
		}
		return strings.ContainsAny(firstRune(line), spinners)
	}
	return false
}

// paneDigest fingerprints a capture so the next refresh can tell whether
// anything moved. Trailing blank lines are dropped first: a pane that only
// scrolled its own whitespace has not done anything.
func paneDigest(lines []string) string {
	h := fnv.New64a()
	for _, line := range trimTrailing(lines) {
		h.Write([]byte(strings.TrimRight(line, " ")))
		h.Write([]byte{'\n'})
	}
	return strconv.FormatUint(h.Sum64(), 36)
}

// paneSummary picks the one line worth showing from a pane capture: what the
// agent in it is doing right now.
//
// Every agent TUI ends its pane with the same furniture — a bordered input
// box, then a status bar — and none of it says anything about the work. So the
// transcript is everything above that box, and the summary is the last line of
// it that the agent wrote about itself.
func paneSummary(lines []string) string {
	transcript := trimTrailing(aboveInputBox(lines))
	if len(transcript) == 0 {
		return ""
	}

	last := len(transcript) - 1
	floor := last - markerLookback
	if floor < 0 {
		floor = 0
	}
	for i := last; i >= floor; i-- {
		line := strings.TrimSpace(transcript[i])
		if line == "" {
			continue
		}
		if strings.ContainsAny(firstRune(line), markers) {
			return clean(line)
		}
	}
	return clean(strings.TrimSpace(transcript[last]))
}

// aboveInputBox returns the transcript: everything above the bordered input
// box an agent TUI parks at the bottom of its pane. A pane with no box at all
// — a shell, a pager, anything that is not an agent — is all transcript.
func aboveInputBox(lines []string) []string {
	bottom := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if isRule(lines[i]) {
			bottom = i
			break
		}
	}
	if bottom < 0 {
		return lines
	}
	for i := bottom - 1; i >= 0; i-- {
		if isRule(lines[i]) {
			return lines[:i]
		}
	}
	return lines[:bottom]
}

// isRule reports whether a line is one of the box's horizontal borders. Agents
// draw them with box-drawing runes and they run the width of the pane, so a
// handful in a row is enough to tell one from prose.
func isRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len([]rune(trimmed)) < 8 {
		return false
	}
	for _, r := range trimmed {
		switch r {
		case '─', '━', '═', '╌', '╭', '╮', '╰', '╯', '│', '┌', '┐', '└', '┘':
		default:
			return false
		}
	}
	return true
}

func trimTrailing(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}

// clean squeezes a captured line down to one readable phrase: runs of spaces
// become one, and the keyboard hints an agent appends to its spinner are
// instructions for whoever is attached rather than news for whoever is not.
func clean(line string) string {
	var b strings.Builder
	space := false
	for _, r := range line {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteRune(' ')
		}
		space = false
		b.WriteRune(r)
	}
	out := b.String()
	for _, hint := range []string{
		" · esc to interrupt", "· esc to interrupt", "(esc to interrupt)",
		" · ctrl+t to hide todos", "(ctrl+c to cancel)",
	} {
		out = strings.ReplaceAll(out, hint, "")
	}
	out = strings.TrimRight(out, " ·(")
	if strings.HasSuffix(out, "()") {
		out = strings.TrimSuffix(out, "()")
	}
	return strings.TrimSpace(out)
}
