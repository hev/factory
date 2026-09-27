package fleet

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrTickBusy is a tick that found another one still running.
var ErrTickBusy = errors.New("another tick is running")

// TickReport is what one tick saw.
type TickReport struct {
	Events []Event
	Died   int // sessions this tick found dead and reported
}

type tickState struct {
	Cursor  int64     `json:"cursor"`
	LastRun time.Time `json:"last_run"`
}

// Tick is the factory's clock: run it every minute on the host that owns the
// jobs. It calls no model. It notices sessions that died, reads every event
// since the last tick, and writes what it saw to ~/.factory/tick.log.
// Waking gaffers for the jobs those events belong to comes later; today a
// tick only reads and records, so an idle factory costs nothing.
func Tick() (TickReport, error) {
	unlock, err := flock(filepath.Join(Home(), "tick.lock"), false)
	if errors.Is(err, errLocked) {
		return TickReport{}, ErrTickBusy
	}
	if err != nil {
		return TickReport{}, err
	}
	defer unlock()

	var rep TickReport
	rep.Died = len(noticeDeaths())

	path := filepath.Join(Home(), "tick.json")
	var ts tickState
	_ = readJSON(path, &ts)
	events, next, err := ReadEvents(ts.Cursor)
	if err != nil {
		return rep, err
	}
	rep.Events = events
	logTick(rep)
	ts.Cursor, ts.LastRun = next, time.Now().UTC()
	return rep, writeJSON(path, ts)
}

// logTick appends one line per event to tick.log, and nothing on an idle
// tick: a minute-by-minute log of "nothing happened" is a log nobody reads.
func logTick(rep TickReport) {
	if len(rep.Events) == 0 {
		return
	}
	f, err := os.OpenFile(filepath.Join(Home(), "tick.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	for _, e := range rep.Events {
		fmt.Fprintln(f, EventLine(e))
	}
}

// EventLine is an event as one readable line.
func EventLine(e Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %-11s %s", e.At.UTC().Format("2006-01-02T15:04:05Z"), e.Kind, e.Session)
	if e.Repo != "" {
		fmt.Fprintf(&b, " %s", e.Repo)
	}
	if e.Turn > 0 {
		fmt.Fprintf(&b, " turn %d", e.Turn)
	}
	if e.Result != "" {
		fmt.Fprintf(&b, ": %s", clip(strings.Join(strings.Fields(e.Result), " "), 160))
	}
	return b.String()
}
