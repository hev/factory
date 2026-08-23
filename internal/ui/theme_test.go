package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

func TestPadIsExactlyWidthCells(t *testing.T) {
	for _, input := range []string{"", "acme", "acme-14-search", "a-very-long-session-name", "💁 reception", "日本語のパス"} {
		for _, width := range []int{1, 6, 14, 20} {
			if got := runewidth.StringWidth(Pad(input, width)); got != width {
				t.Errorf("Pad(%q, %d) is %d cells wide, want %d", input, width, got, width)
			}
		}
	}
	if got := Pad("anything", 0); got != "" {
		t.Errorf("Pad(_, 0) = %q, want empty", got)
	}
}

func TestDuration(t *testing.T) {
	for seconds, want := range map[int]string{0: "now", 59: "now", 60: "1m", 3599: "59m", 3600: "1h", 86399: "23h", 86400: "1d"} {
		if got := Duration(seconds); got != want {
			t.Errorf("Duration(%d) = %q, want %q", seconds, got, want)
		}
	}
}

func TestInstanceStyleIsStable(t *testing.T) {
	if InstanceStyle("acme").GetForeground() != InstanceStyle("acme").GetForeground() {
		t.Error("an instance must keep the same accent between renders")
	}
	t.Setenv("FACTORY_INSTANCE_COLORS", "acme=#ff0000")
	pinned = nil
	if got := InstanceStyle("acme").GetForeground(); got != lipgloss.Color("#ff0000") {
		t.Errorf("FACTORY_INSTANCE_COLORS should win, got %v", got)
	}
	pinned = nil
}
