package fleet

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEventsCursor(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	if ev, next, err := ReadEvents(0); err != nil || len(ev) != 0 || next != 0 {
		t.Fatalf("no file: %v %d %v", ev, next, err)
	}
	emit(Event{Kind: EvStarted, Session: "a"})
	emit(Event{Kind: EvTurnDone, Session: "a", Turn: 1, Result: "opened #12"})
	ev, next, err := ReadEvents(0)
	if err != nil || len(ev) != 2 || ev[1].Result != "opened #12" || ev[0].At.IsZero() {
		t.Fatalf("read: %+v %v", ev, err)
	}
	if again, n2, _ := ReadEvents(next); len(again) != 0 || n2 != next {
		t.Fatalf("cursor re-read %d events", len(again))
	}

	// A line still being written stays for the next read.
	f, _ := os.OpenFile(eventsPath(), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"kind":"killed","sess`)
	f.Close()
	if part, n3, _ := ReadEvents(next); len(part) != 0 || n3 != next {
		t.Fatalf("partial line read as %d events, cursor %d→%d", len(part), next, n3)
	}
	f, _ = os.OpenFile(eventsPath(), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`ion":"a"}` + "\n")
	f.Close()
	if done, _, _ := ReadEvents(next); len(done) != 1 || done[0].Kind != EvKilled {
		t.Fatalf("completed line: %+v", done)
	}

	// A replaced (shorter) file starts from the top.
	os.WriteFile(eventsPath(), []byte(`{"kind":"started","session":"b"}`+"\n"), 0o644)
	if fresh, _, _ := ReadEvents(next); len(fresh) != 1 || fresh[0].Session != "b" {
		t.Fatalf("replaced file: %+v", fresh)
	}
}

func TestTickReportsADeathOnce(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	id := "dead01"
	os.MkdirAll(sessionDir(id), 0o755)
	writeJSON(filepath.Join(sessionDir(id), "meta.json"), Meta{ID: id, Repo: "hev/lyr", Branch: "factory/dead01"})
	writeJSON(filepath.Join(sessionDir(id), "state.json"), State{Status: Running, Turns: 1, UpdatedAt: time.Now().Add(-time.Minute)})

	rep, err := Tick()
	if err != nil || rep.Died != 1 || len(rep.Events) != 1 || rep.Events[0].Kind != EvDied || rep.Events[0].Repo != "hev/lyr" {
		t.Fatalf("first tick: %+v %v", rep, err)
	}
	if loadState(id).Status != Died {
		t.Fatalf("state not recorded as died: %+v", loadState(id))
	}
	rep, err = Tick()
	if err != nil || rep.Died != 0 || len(rep.Events) != 0 {
		t.Fatalf("second tick saw it again: %+v %v", rep, err)
	}

	// A start saved a moment ago is not yet dead.
	fresh := "fresh1"
	os.MkdirAll(sessionDir(fresh), 0o755)
	writeJSON(filepath.Join(sessionDir(fresh), "meta.json"), Meta{ID: fresh})
	saveState(fresh, State{Status: Running})
	if rep, _ := Tick(); rep.Died != 0 {
		t.Fatalf("a session saved just now was called dead")
	}
}
