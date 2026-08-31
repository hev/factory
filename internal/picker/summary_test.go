package picker

import (
	"sync"
	"testing"
)

func TestDetachedSessionDoesNotScheduleSummary(t *testing.T) {
	oldVisible := sessionVisible
	oldEnabled := enabled
	oldSummaries := summaries
	t.Cleanup(func() {
		sessionVisible = oldVisible
		enabled = oldEnabled
		enabledOnce = sync.Once{}
		summaries = oldSummaries
	})

	sessionVisible = func() bool { return false }
	enabled = true
	enabledOnce.Do(func() {})
	summaries = &summarizer{entries: map[string]summaryEntry{}, active: true}

	if got, _ := summaries.label("worker-acme-1-test", "changed", []string{"working"}, true); got != "" {
		t.Fatalf("label = %q, want empty", got)
	}
	entry := summaries.entries["worker-acme-1-test"]
	if entry.inFlight || !entry.tried.IsZero() {
		t.Fatalf("detached summary was scheduled: %+v", entry)
	}
}
