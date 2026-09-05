package machine

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hev/factory/internal/ui"
)

// Line is the box in one phrase, for the picker's header:
//
//	cpu 41%  ·  mem 6.2/16G  ·  load 2.1  ·  up 9d
//
// Empty until the first sample has landed, so the header simply does not carry
// the segment rather than carrying zeroes it has not measured.
//
// Each number colours itself. The whole point of putting these on a screen
// about agents is the moment one of them is the reason the agents are slow,
// and a phrase drawn in one uniform grey makes that moment look like every
// other moment.
func (s Stats) Line() string {
	if !s.Known {
		return ""
	}
	parts := []string{
		cpuStyle(s.CPU).Render(fmt.Sprintf("cpu %.0f%%", s.CPU)),
		memStyle(s).Render("mem " + s.memPhrase()),
		loadStyle(s).Render(fmt.Sprintf("load %.1f", s.Load1)),
	}
	if s.Uptime > 0 {
		parts = append(parts, ui.Dim.Render("up "+ui.Duration(int(s.Uptime.Seconds()))))
	}
	return strings.Join(parts, ui.Dim.Render(" · "))
}

// Compact is the same reading with the two least actionable parts given up:
// uptime, and the absolute memory figures in favour of a percentage.
//
// It exists so a 150-column terminal can carry the box *and* the keys hint
// rather than choosing. The full form is better when there is room, because
// "6.2 of 16G" is a number somebody can reason about and "39%" is not, but
// losing the keys strip to keep the G's would be a bad trade on the screen
// most people actually have open.
func (s Stats) Compact() string {
	if !s.Known {
		return ""
	}
	parts := []string{
		cpuStyle(s.CPU).Render(fmt.Sprintf("cpu %.0f%%", s.CPU)),
		memStyle(s).Render("mem " + s.memPercent()),
		loadStyle(s).Render(fmt.Sprintf("load %.1f", s.Load1)),
	}
	return strings.Join(parts, ui.Dim.Render(" · "))
}

func (s Stats) memPercent() string {
	if s.MemTotal == 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(s.MemUsed)/float64(s.MemTotal))
}

// memPhrase is used-of-installed in gigabytes, which is how everybody reads
// memory out loud. The used half keeps a decimal and the installed half does
// not: 16G is a property of the machine and 6.2G is the news.
func (s Stats) memPhrase() string {
	const gig = 1 << 30
	if s.MemTotal == 0 {
		return fmt.Sprintf("%.1fG", float64(s.MemUsed)/gig)
	}
	return fmt.Sprintf("%.1f/%.0fG", float64(s.MemUsed)/gig, float64(s.MemTotal)/gig)
}

// The thresholds. They are about what an operator would *do*, not about round
// numbers: yellow is "this is why your agents feel slow" and red is "the next
// worker dispatched here will not get a fair share".
const (
	cpuBusy      = 70.0
	cpuSaturated = 90.0
	memBusy      = 0.80
	memFull      = 0.92
	// Load is per core, so the same numbers mean the same thing on a laptop
	// and on the mini. One runnable process per core is a machine with no
	// slack left; two is a queue.
	loadBusy = 1.0
	loadFull = 2.0
)

func cpuStyle(pct float64) lipgloss.Style { return band(pct, cpuBusy, cpuSaturated) }

func memStyle(s Stats) lipgloss.Style {
	if s.MemTotal == 0 {
		return ui.Dim
	}
	return band(float64(s.MemUsed)/float64(s.MemTotal), memBusy, memFull)
}

func loadStyle(s Stats) lipgloss.Style {
	if s.Cores == 0 {
		return ui.Dim
	}
	return band(s.Load1/float64(s.Cores), loadBusy, loadFull)
}

// band is the one rule all three share: quiet until it matters, and two steps
// after that. Trouble and Waiting rather than Alarm, because a busy box is the
// same *kind* of news as a worker that stopped to ask something — worth
// walking towards, not worth pulling the cord over.
func band(value, busy, full float64) lipgloss.Style {
	switch {
	case value >= full:
		return ui.Trouble
	case value >= busy:
		return ui.Waiting
	}
	return ui.Dim
}
