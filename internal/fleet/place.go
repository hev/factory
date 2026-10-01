package fleet

import (
	"fmt"
)

// Room says why this machine can't take another session, or nil. It has
// room when it runs the harness, has a live-session slot free, is not already
// loaded past its cores, has memory to spare, and has not spent the week's
// subscription. `run` refuses rather than oversubscribe.
func Room(in Info, harness string) error {
	if reason := noRoom(in, harness); reason != "" {
		return fmt.Errorf("no room on %s: %s", in.Name, reason)
	}
	return nil
}

func noRoom(in Info, harness string) string {
	if !contains(in.Harnesses, harness) {
		return harness + " is not installed"
	}
	if max := Slots(in); in.Live >= max {
		return fmt.Sprintf("%d of %d sessions live", in.Live, max)
	}
	if in.Load > 0.9*float64(in.Cores) {
		return fmt.Sprintf("load %.1f on %d cores", in.Load, in.Cores)
	}
	if in.MemFreePct >= 0 && in.MemFreePct < 10 {
		return fmt.Sprintf("%d%% memory free", in.MemFreePct)
	}
	if u := in.Usage[harness]; u != nil && u.UsedPct >= 95 {
		return fmt.Sprintf("%s at %.0f%% of its week", harness, u.UsedPct)
	}
	return ""
}

// Slots allows one live session per core. Agents often wait on remote models
// or CI; Room separately guards CPU load, free memory, and subscription use.
func Slots(in Info) int { return max(1, in.Cores) }

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
