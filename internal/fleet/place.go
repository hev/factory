package fleet

import (
	"fmt"
	"strings"
)

// Candidate is a host with what it just reported about itself.
type Candidate struct {
	Host Host
	Info *Info
	Err  error
}

// Place picks the host for a new session: the always-on hosts first, in the
// order they were added, then this machine as overflow. A host has room when
// it runs the harness, has a live-session slot free, is not already loaded
// past its cores, has memory to spare, and has not spent the week's
// subscription. When nothing has room it says why for each host rather than
// oversubscribe one.
func Place(cands []Candidate, harness string) (Host, error) {
	ordered := append([]Candidate{}, cands[1:]...)
	ordered = append(ordered, cands[0]) // cands[0] is always the local host
	var why []string
	for _, c := range ordered {
		if reason := noRoom(c, harness); reason != "" {
			why = append(why, c.Host.Name+": "+reason)
			continue
		}
		return c.Host, nil
	}
	return Host{}, fmt.Errorf("no host has room:\n  %s", strings.Join(why, "\n  "))
}

func noRoom(c Candidate, harness string) string {
	if c.Err != nil {
		return "unreachable"
	}
	in := c.Info
	if !contains(in.Harnesses, harness) {
		return harness + " is not installed"
	}
	if max := Slots(c.Host, in); in.Live >= max {
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

// Slots is how many live sessions a host takes.
func Slots(h Host, in *Info) int {
	if h.Max > 0 {
		return h.Max
	}
	return max(1, in.Cores/2)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
