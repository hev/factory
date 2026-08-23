package picker

import "testing"

func TestMatches(t *testing.T) {
	const row = "acme-14-search acme agent-ready-pr-handoff /Users/you/workspace/acme"

	for _, query := range []string{"", "acme", "14", "search", "a14s", "handoff", "ACME", "workspace"} {
		if !matches(row, query) {
			t.Errorf("matches(row, %q) = false, want true", query)
		}
	}
	for _, query := range []string{"bcc", "zzz", "search14"} {
		if matches(row, query) {
			t.Errorf("matches(row, %q) = true, want false", query)
		}
	}
}
