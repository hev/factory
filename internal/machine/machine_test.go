package machine

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/hev/factory/internal/ui"
)

// kern.boottime is printed as a struct and then, on macOS, as a localised date
// on the same line. The seconds are taken out of the struct precisely because
// the date beside them is not something to parse.
func TestBootTimeReadsTheStructNotTheDate(t *testing.T) {
	got := bootTime("{ sec = 1788089519, usec = 829380 } Sun Aug 30 05:31:59 2026")
	if want := time.Unix(1788089519, 0); !got.Equal(want) {
		t.Errorf("boot time = %v, want %v", got, want)
	}
	for _, junk := range []string{"", "not a struct", "{ sec = , usec = 0 }", "{ sec = -1 }"} {
		if !bootTime(junk).IsZero() {
			t.Errorf("bootTime(%q) should be zero", junk)
		}
	}
}

// Active, wired and compressed are counted; inactive is deliberately not.
// Inactive is the largest number in vm_stat on a machine that has been up a
// week and is reclaimable on demand, and counting it is how a header reports
// 94% on an idle box and teaches everybody to ignore it.
func TestParseVMStatCountsWhatCannotBeGivenAway(t *testing.T) {
	dump := strings.Split(`Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                4853.
Pages active:                                100.
Pages inactive:                           999999.
Pages speculative:                           379.
Pages wired down:                             10.
Pages purgeable:                            6264.
Pages occupied by compressor:                  1.
`, "\n")

	const page = 16384
	if got, want := parseVMStat(dump), uint64(111*page); got != want {
		t.Errorf("used = %d, want %d", got, want)
	}
	if got := parseVMStat(nil); got != 0 {
		t.Errorf("no dump should read as 0, got %d", got)
	}
}

// The header is one phrase, and it says nothing at all until something has
// been measured — a screen that reports 0% before it has looked is a screen
// that lies for its first five seconds.
func TestLineIsEmptyUntilTheFirstSample(t *testing.T) {
	if got := (Stats{}).Line(); got != "" {
		t.Errorf("unmeasured line = %q, want empty", got)
	}
	line := Stats{Known: true, CPU: 41, Cores: 10, Load1: 2.1,
		MemUsed: 6 * 1 << 30, MemTotal: 16 * 1 << 30, Uptime: 9 * 24 * time.Hour}.Line()
	for _, want := range []string{"cpu 41%", "6.0/16G", "load 2.1", "up 9d"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q is missing %q", line, want)
		}
	}
}

// Load is banded per core so the same number means the same thing on a laptop
// and on a mini, and CPU and memory band on their own scales.
//
// The styles are compared by their colour rather than by rendering them: a
// test process has no terminal, so lipgloss correctly strips every escape and
// three different bands render as the same plain string.
func TestBandsRiseOnlyWhenSomebodyWouldCare(t *testing.T) {
	quiet := Stats{Known: true, Cores: 10, Load1: 2, CPU: 20, MemUsed: 1, MemTotal: 100}
	busy := Stats{Known: true, Cores: 10, Load1: 12, CPU: 75, MemUsed: 85, MemTotal: 100}
	full := Stats{Known: true, Cores: 10, Load1: 25, CPU: 95, MemUsed: 95, MemTotal: 100}

	for _, tc := range []struct {
		name string
		got  lipgloss.TerminalColor
		want lipgloss.TerminalColor
	}{
		{"quiet load", loadStyle(quiet).GetForeground(), ui.Dim.GetForeground()},
		{"quiet cpu", cpuStyle(quiet.CPU).GetForeground(), ui.Dim.GetForeground()},
		{"quiet mem", memStyle(quiet).GetForeground(), ui.Dim.GetForeground()},

		{"busy load", loadStyle(busy).GetForeground(), ui.Waiting.GetForeground()},
		{"busy cpu", cpuStyle(busy.CPU).GetForeground(), ui.Waiting.GetForeground()},
		{"busy mem", memStyle(busy).GetForeground(), ui.Waiting.GetForeground()},

		{"full load", loadStyle(full).GetForeground(), ui.Trouble.GetForeground()},
		{"full cpu", cpuStyle(full.CPU).GetForeground(), ui.Trouble.GetForeground()},
		{"full mem", memStyle(full).GetForeground(), ui.Trouble.GetForeground()},

		// A box that has not said how many cores it has cannot have its load
		// banded against them, and guessing is worse than staying quiet.
		{"load with no cores", loadStyle(Stats{Known: true, Load1: 99}).GetForeground(), ui.Dim.GetForeground()},
		{"memory with no total", memStyle(Stats{Known: true, MemUsed: 99}).GetForeground(), ui.Dim.GetForeground()},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}
