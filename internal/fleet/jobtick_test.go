package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeWorld stands in for gaffer sessions and GitHub.
type fakeWorld struct {
	started []string // job ids a gaffer was started for
	sent    []string // texts sent to gaffers
	prs     map[string]*prView
}

func newFakeWorld(t *testing.T) *fakeWorld {
	w := &fakeWorld{prs: map[string]*prView{}}
	oldStart, oldSend, oldView := startGafferFn, sendFn, viewPRFn
	t.Cleanup(func() { startGafferFn, sendFn, viewPRFn = oldStart, oldSend, oldView })
	startGafferFn = func(j Job, events string) (Meta, error) {
		w.started = append(w.started, j.ID)
		id := "gaf" + NewID()[:3]
		os.MkdirAll(sessionDir(id), 0o755)
		m := Meta{ID: id, Kind: KindGaffer, Job: j.ID}
		writeJSON(filepath.Join(sessionDir(id), "meta.json"), m)
		saveState(id, State{Status: Done})
		return m, nil
	}
	sendFn = func(id, msg string) (string, error) { w.sent = append(w.sent, msg); return "queued", nil }
	viewPRFn = func(repo, branch string) *prView { return w.prs[branch] }
	return w
}

// fakePartSession records a session for a job's part, as StartPart would.
func fakePartSession(t *testing.T, jobID, part, repo string) string {
	id := "p" + part
	os.MkdirAll(sessionDir(id), 0o755)
	writeJSON(filepath.Join(sessionDir(id), "meta.json"), Meta{ID: id, Repo: repo, Branch: "factory/" + id, Job: jobID, Part: part})
	saveState(id, State{Status: Done, Turns: 1})
	updateJobState(jobID, func(st *JobState) error {
		st.Parts[part] = PartState{Session: id, Status: PartRunning}
		return nil
	})
	return id
}

func tickT(t *testing.T) TickReport {
	t.Helper()
	rep, err := Tick()
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestTickDrivesAJob(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	w := newFakeWorld(t)
	j, err := AddJob(JobSpec{Ask: "ordered scan", Parts: []Part{
		{Name: "scan", Repo: "hev/layer-pro", Task: "implement"},
		{Name: "docs", Repo: "hev/lyr", Task: "document", After: []string{"scan"}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	// A new job gets a gaffer at once.
	rep := tickT(t)
	if len(w.started) != 1 || len(rep.Wakes) != 1 || !rep.Wakes[0].Started {
		t.Fatalf("first tick: started %v, wakes %+v", w.started, rep.Wakes)
	}
	// Nothing changed: no wake, no model.
	if rep := tickT(t); len(rep.Wakes) != 0 || len(w.sent) != 0 {
		t.Fatalf("idle tick woke: %+v", rep.Wakes)
	}

	// The scan part ends a turn with an open PR: one wake with both.
	s := fakePartSession(t, j.ID, "scan", "hev/layer-pro")
	emitFor(Meta{ID: s, Repo: "hev/layer-pro", Branch: "factory/" + s, Job: j.ID, Part: "scan"}, EvTurnDone, State{Turns: 1, Result: "Opened #627."})
	w.prs["factory/"+s] = &prView{Number: 627, URL: "https://github.com/hev/layer-pro/pull/627", State: "OPEN",
		Checks: []check{{Name: "ci", Status: "COMPLETED", Conclusion: "FAILURE"}}}
	tickT(t)
	if len(w.sent) != 1 || !strings.Contains(w.sent[0], "ended turn 1: Opened #627.") || !strings.Contains(w.sent[0], "checks failing: ci") {
		t.Fatalf("wake text: %q", w.sent)
	}
	j, _ = LoadJob(j.ID)
	if ps := j.State.Parts["scan"]; ps.Status != PartIdle || ps.PR == nil || ps.PR.Number != 627 {
		t.Fatalf("scan part: %+v", ps)
	}
	// Same PR next minute: nothing new.
	if tickT(t); len(w.sent) != 1 {
		t.Fatalf("unchanged PR woke the gaffer again")
	}

	// A comment from the operator, and one from the bot itself.
	loginOnce.Do(func() {})
	login = "hevbot" // this host acts as hevbot; its own comments are not news
	w.prs["factory/"+s].Comments = []prComment{{Body: "bot note"}, {Body: "use COLLATE C"}}
	w.prs["factory/"+s].Comments[0].Author.Login = "hevbot"
	w.prs["factory/"+s].Comments[1].Author.Login = "hev"
	tickT(t)
	if last := w.sent[len(w.sent)-1]; !strings.Contains(last, "@hev: use COLLATE C") || strings.Contains(last, "bot note") {
		t.Fatalf("comment wake: %q", last)
	}

	// scan merges: the part settles and docs becomes ready, in one wake.
	w.prs["factory/"+s].State = "MERGED"
	tickT(t)
	j, _ = LoadJob(j.ID)
	last := w.sent[len(w.sent)-1]
	if j.State.Parts["scan"].Status != PartMerged || j.State.Parts["docs"].Status != PartReady || !strings.Contains(last, "Part docs is ready") {
		t.Fatalf("after merge: %+v %q", j.State.Parts, last)
	}

	// docs merges too: the job is done, and nobody is woken for it.
	d := fakePartSession(t, j.ID, "docs", "hev/lyr")
	w.prs["factory/"+d] = &prView{Number: 9, State: "MERGED"}
	before := len(w.sent)
	tickT(t)
	j, _ = LoadJob(j.ID)
	if j.State.Status != JobDone || len(w.sent) != before {
		t.Fatalf("done: status %s, woke %d more", j.State.Status, len(w.sent)-before)
	}
}

func TestTickCeilingOperatorAndGafferLog(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	w := newFakeWorld(t)
	j, _ := AddJob(JobSpec{Ask: "small", Ceiling: Ceiling{Wakes: 2, Days: 1}})
	tickT(t) // wake 1: the gaffer starts
	j, _ = LoadJob(j.ID)
	g := j.State.Gaffer

	// The gaffer's own finished turn goes to the log, and wakes nobody.
	emit(Event{Kind: EvTurnDone, Session: g, Job: j.ID, Gaffer: true, Result: "Split into two parts; waiting on scan."})
	if rep := tickT(t); len(rep.Wakes) != 0 {
		t.Fatalf("gaffer's own turn woke it: %+v", rep.Wakes)
	}
	if log, _ := JobLog(j.ID, 5); !strings.Contains(log, "Split into two parts") {
		t.Fatalf("gaffer turn not logged: %s", log)
	}

	// Waiting on the operator; the operator answers: reopened and woken.
	SetJobStatus(j.ID, JobWaiting, "which region?", "gaffer")
	Say(j.ID, "us-east-1", "hev")
	tickT(t) // wake 2
	j, _ = LoadJob(j.ID)
	if j.State.Status != JobOpen || !strings.Contains(w.sent[len(w.sent)-1], "The operator says: us-east-1") {
		t.Fatalf("operator answer: %s %q", j.State.Status, w.sent)
	}

	// Past two wakes, the next change stops the job instead of waking it.
	Say(j.ID, "also eu", "hev")
	before := len(w.sent)
	tickT(t)
	j, _ = LoadJob(j.ID)
	if j.State.Status != JobStopped || len(w.sent) != before || !strings.Contains(j.State.WaitingOnYou+mustLog(j.ID), "2 wakes") {
		t.Fatalf("ceiling: %s, woke %d", j.State.Status, len(w.sent)-before)
	}
	if j, _ = RaiseCeiling(j.ID, 10, 0, "hev"); j.State.Status != JobOpen || j.Ceiling.Wakes != 10 {
		t.Fatalf("raise: %+v", j.State)
	}
	_ = time.Now
}

func mustLog(id string) string { l, _ := JobLog(id, 10); return l }

// A task that named its own branch moved the session off factory/<id>; its
// pull request is still found, and settles a job that was waiting on it.
func TestTickFindsAPRWhereTheSessionMoved(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	w := newFakeWorld(t)
	j, _ := AddJob(JobSpec{Ask: "blobs", Parts: []Part{{Name: "blobs", Repo: "hev/layer-pro", Task: "implement"}}})
	tickT(t)
	s := fakePartSession(t, j.ID, "blobs", "hev/layer-pro")

	work := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "factory/" + s}, {"checkout", "-q", "-b", "hevbot/blobs"}} {
		if out, err := git(work, args...); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	writeJSON(filepath.Join(sessionDir(s), "meta.json"), Meta{ID: s, Repo: "hev/layer-pro", Branch: "factory/" + s, Worktree: work, Job: j.ID, Part: "blobs"})
	updateJobState(j.ID, func(st *JobState) error {
		st.Parts["blobs"] = PartState{Session: s, Status: PartKilled}
		return nil
	})
	SetJobStatus(j.ID, JobWaiting, "no PR on factory/"+s, "gaffer")

	w.prs["hevbot/blobs"] = &prView{Number: 657, State: "MERGED"}
	tickT(t)
	j, _ = LoadJob(j.ID)
	if ps := j.State.Parts["blobs"]; ps.Status != PartMerged || ps.PR == nil || ps.PR.Number != 657 || j.State.Status != JobDone {
		t.Fatalf("moved branch: job %s, part %+v", j.State.Status, ps)
	}
}
