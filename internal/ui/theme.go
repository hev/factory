// Package ui holds the picker's palette and the column arithmetic every row
// shares. Rows are fixed-width by hand rather than by a table layout: the
// screen is read at a glance, and columns that shift as sessions come and go
// are harder to read than columns that sometimes hold blanks.
package ui

import (
	"hash/fnv"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// Catppuccin Mocha, the palette the rest of the factory's terminal output uses.
var (
	Text    = lipgloss.Color("#cdd6f4")
	Subtext = lipgloss.Color("#6c7086")
	Green   = lipgloss.Color("#a6e3a1")
	Red     = lipgloss.Color("#f38ba8")
	Yellow  = lipgloss.Color("#f9e2af")
	Mauve   = lipgloss.Color("#cba6f7")
	Pink    = lipgloss.Color("#f5c2e7")
	Sky     = lipgloss.Color("#89dceb")
	Overlay = lipgloss.Color("#585b70")
	Surface = lipgloss.Color("#313244")
)

var (
	Dim      = lipgloss.NewStyle().Foreground(Subtext)
	Normal   = lipgloss.NewStyle().Foreground(Text)
	Working  = lipgloss.NewStyle().Foreground(Green)
	Attached = lipgloss.NewStyle().Foreground(Green)
	Agent    = lipgloss.NewStyle().Foreground(Sky)
	Issue    = lipgloss.NewStyle().Foreground(Sky)
	Alarm    = lipgloss.NewStyle().Foreground(Red).Bold(true)
	Accent   = lipgloss.NewStyle().Foreground(Pink)
	NewLabel = lipgloss.NewStyle().Foreground(Mauve)
	Header   = lipgloss.NewStyle().Foreground(Subtext)
	Selected = lipgloss.NewStyle().Background(Surface)
	Match    = lipgloss.NewStyle().Foreground(Pink).Bold(true)
	Confirm  = lipgloss.NewStyle().Foreground(Red).Bold(true).
			Border(lipgloss.RoundedBorder()).BorderForeground(Overlay).Padding(0, 2)
	Flash = lipgloss.NewStyle().Foreground(Green)
)

// instancePalette is the set of accents a factory instance can be drawn in.
// Which one an instance gets is decided by its name rather than by a table
// somebody has to edit every time a factory is added.
var instancePalette = []lipgloss.Color{
	lipgloss.Color("#89b4fa"), // blue
	lipgloss.Color("#f5c2e7"), // pink
	lipgloss.Color("#fab387"), // peach
	lipgloss.Color("#a6e3a1"), // green
	lipgloss.Color("#cba6f7"), // mauve
	lipgloss.Color("#f9e2af"), // yellow
	lipgloss.Color("#94e2d5"), // teal
}

// InstanceStyle is the accent for one factory instance. FACTORY_INSTANCE_COLORS
// pins specific ones — "acme=#89b4fa,docs=#f5c2e7" — and anything unpinned
// gets a stable colour derived from its name.
func InstanceStyle(name string) lipgloss.Style {
	if name == "" {
		return Dim
	}
	if pinned, ok := pinnedColors()[name]; ok {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(pinned))
	}
	hash := fnv.New32a()
	hash.Write([]byte(name))
	return lipgloss.NewStyle().Foreground(instancePalette[hash.Sum32()%uint32(len(instancePalette))])
}

var pinned map[string]string

func pinnedColors() map[string]string {
	if pinned != nil {
		return pinned
	}
	pinned = map[string]string{}
	for _, pair := range strings.Split(os.Getenv("FACTORY_INSTANCE_COLORS"), ",") {
		name, color, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && name != "" && color != "" {
			pinned[name] = color
		}
	}
	return pinned
}

// Pad fits s to exactly width cells, truncating with an ellipsis when it is
// too long. Width is measured in terminal cells, so an emoji or a CJK path
// still lands on the column it should.
func Pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) > width {
		// Truncating can land a cell short when the character that would not
		// fit is double-width, so the pad below is not optional.
		s = runewidth.Truncate(s, width, "…")
	}
	if gap := width - runewidth.StringWidth(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// Duration renders seconds the way a glance wants them: "now" while something
// is still moving, then one significant unit.
func Duration(seconds int) string {
	switch {
	case seconds < 60:
		return "now"
	case seconds < 3600:
		return strconv.Itoa(seconds/60) + "m"
	case seconds < 86400:
		return strconv.Itoa(seconds/3600) + "h"
	default:
		return strconv.Itoa(seconds/86400) + "d"
	}
}
