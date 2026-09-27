package fleet

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Event kinds. A session writes the first four itself. Nothing is left to
// write the fifth when a session dies, so tick writes it on noticing.
const (
	EvStarted    = "started"     // factory run made the session
	EvTurnDone   = "turn_done"   // a turn ended cleanly; Result is what it said
	EvTurnFailed = "turn_failed" // a turn ended in an error
	EvKilled     = "killed"      // factory kill
	EvDied       = "died"        // it was running, and nothing is any more
	EvOperator   = "operator"    // the operator said something to a job (factory job say)
)

// Event is one line of ~/.factory/events.jsonl: something a session did that
// whoever coordinates it may want to act on. Events are facts, written once
// and never rewritten. Reading them costs nothing and calls no model.
type Event struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Session string    `json:"session"`
	Job     string    `json:"job,omitempty"`
	Part    string    `json:"part,omitempty"`
	Gaffer  bool      `json:"gaffer,omitempty"` // the job's coordinator, not one of its parts
	Repo    string    `json:"repo,omitempty"`
	Branch  string    `json:"branch,omitempty"`
	Turn    int       `json:"turn,omitempty"`
	Exit    int       `json:"exit,omitempty"`
	Result  string    `json:"result,omitempty"`
}

func eventsPath() string { return filepath.Join(Home(), "events.jsonl") }

// emit appends an event. It never fails a session: an event that cannot be
// written is reported on stderr and dropped, because the session's own
// state.json still records where it got to.
func emit(e Event) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	e.Result = clip(e.Result, 2000)
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	if err := os.MkdirAll(Home(), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "factory: events:", err)
		return
	}
	unlock, err := flock(eventsPath()+".lock", true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "factory: events:", err)
		return
	}
	defer unlock()
	f, err := os.OpenFile(eventsPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "factory: events:", err)
		return
	}
	defer f.Close()
	f.Write(append(line, '\n'))
}

func emitFor(m Meta, kind string, st State) {
	emit(Event{Kind: kind, Session: m.ID, Job: m.Job, Part: m.Part, Gaffer: m.Kind == KindGaffer,
		Repo: m.Repo, Branch: m.Branch, Turn: st.Turns, Exit: st.Exit, Result: st.Result})
}

// ReadEvents returns the events after byte offset from, and the offset to
// read from next time. A partial last line (a write in progress) is left for
// the next read.
func ReadEvents(from int64) ([]Event, int64, error) {
	f, err := os.Open(eventsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, from, err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() < from {
		from = 0 // the file was replaced; start again
	}
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, from, err
	}
	var out []Event
	r := bufio.NewReaderSize(f, 1<<16)
	next := from
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // EOF, or a line still being written
		}
		next += int64(len(line))
		var e Event
		if json.Unmarshal(line, &e) == nil {
			out = append(out, e)
		}
	}
	return out, next, nil
}

// noticeDeaths records as died every session that says it is running but has
// no runner and no tmux session, and emits the event for each. It is how a
// death becomes an event: the session cannot report its own.
func noticeDeaths() []Event {
	entries, _ := os.ReadDir(sessionsDir())
	var out []Event
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		// A session that was saved as running a moment ago may not have its
		// tmux session yet. Give a start ten seconds before calling it dead.
		if st := loadState(id); st.Status != Running || time.Since(st.UpdatedAt) < 10*time.Second || liveState(id).Status != Died {
			continue
		}
		m, err := loadMeta(id)
		if err != nil {
			continue
		}
		st, err := updateState(id, func(s *State) {
			if s.Status == Running && !runnerAlive(id) && len(tmuxSessions(id)) == 0 {
				s.Status = Died
			}
		})
		if err != nil || st.Status != Died {
			continue
		}
		ev := Event{At: time.Now().UTC(), Kind: EvDied, Session: id, Job: m.Job, Part: m.Part, Gaffer: m.Kind == KindGaffer,
			Repo: m.Repo, Branch: m.Branch, Turn: st.Turns}
		emit(ev)
		out = append(out, ev)
	}
	return out
}
