package picker

import "testing"

// The tag decides whether a row turns red, so what a model writes has to
// decode the same way every time — including the shapes it writes when it is
// not following the instruction.
func TestSplitVerdict(t *testing.T) {
	cases := []struct {
		line   string
		health Health
		text   string
	}{
		{"trouble: npm test has failed the same way three times", HealthTrouble,
			"npm test has failed the same way three times"},
		{"waiting: asks which index to rebuild first", HealthWaiting,
			"asks which index to rebuild first"},
		{"ok: rebasing intake-inbox onto main", HealthOK, "rebasing intake-inbox onto main"},
		{"OK:  padded and capitalised", HealthOK, "padded and capitalised"},

		// A label from the cache written before verdicts existed. It is still
		// the best thing anyone has to say about that row, so it is kept whole
		// rather than parsed into nothing.
		{"Rebased onto main; ready for review", HealthUnknown, "Rebased onto main; ready for review"},

		// A colon that is part of the sentence is not a tag. Cutting on it
		// anyway would silently eat the first clause of the label.
		{"running Bash: npm test -- --watch", HealthUnknown, "running Bash: npm test -- --watch"},

		// A tag and nothing after it says nothing; the line is worth more.
		{"trouble:", HealthTrouble, "trouble:"},
	}

	for _, c := range cases {
		health, text := splitVerdict(c.line)
		if health != c.health || text != c.text {
			t.Errorf("splitVerdict(%q) = %v, %q; want %v, %q", c.line, health, text, c.health, c.text)
		}
	}
}

func TestAttentionIsOnlyTheTwoThatWantSomebody(t *testing.T) {
	for health, want := range map[Health]bool{
		HealthUnknown: false,
		HealthOK:      false,
		HealthWaiting: true,
		HealthTrouble: true,
	} {
		if got := health.Attention(); got != want {
			t.Errorf("%v.Attention() = %v, want %v", health, got, want)
		}
	}
}

// One cell, three things that could claim it. The order is what somebody would
// do about them, and the panel carries whichever ones lost.
func TestMarkRanksTroubleOverStaleOverWaiting(t *testing.T) {
	trouble := mark(agentRow{Health: HealthTrouble, Stale: true})
	stale := mark(agentRow{Health: HealthWaiting, Stale: true})
	waiting := mark(agentRow{Health: HealthWaiting})
	quiet := mark(agentRow{Health: HealthOK})

	if !contains(trouble, "!") {
		t.Errorf("a worker in trouble and stale should mark trouble, got %q", trouble)
	}
	if !contains(stale, "⚠") {
		t.Errorf("a stale worker that is only waiting should mark stale, got %q", stale)
	}
	if !contains(waiting, "?") {
		t.Errorf("a waiting worker should mark waiting, got %q", waiting)
	}
	if contains(quiet, "!") || contains(quiet, "⚠") || contains(quiet, "?") {
		t.Errorf("a healthy worker should mark nothing, got %q", quiet)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// The on-disk cache outlives the prompt that wrote it. An entry from before
// verdicts existed still has to hand the row both halves of the answer.
func TestCachedLabelsFromTheOldPromptStillDecode(t *testing.T) {
	text, health := readVerdict(summaryEntry{Text: "waiting: Quiet beat, PR #40 in the user's court"})
	if health != HealthWaiting {
		t.Errorf("an old waiting: label lost its verdict, got %v", health)
	}
	if text != "Quiet beat, PR #40 in the user's court" {
		t.Errorf("an old waiting: label kept its prefix: %q", text)
	}

	text, health = readVerdict(summaryEntry{Text: "Rebased onto main", Tag: "ok"})
	if health != HealthOK || text != "Rebased onto main" {
		t.Errorf("a tagged entry should come back as written, got %v / %q", health, text)
	}

	text, health = readVerdict(summaryEntry{Text: "Rebased onto main"})
	if health != HealthUnknown || text != "Rebased onto main" {
		t.Errorf("an untagged plain label should survive whole, got %v / %q", health, text)
	}
}
