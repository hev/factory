package fleet

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seamOnPath puts a shell script named name on PATH, the way a build adds a
// seam, and returns the file the script records its calls in.
func seamOnPath(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nCALLS=" + calls + "\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls
}

func read(path string) string { b, _ := os.ReadFile(path); return string(b) }

func TestIntakeFilesOncePerSource(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	w := newFakeWorld(t)
	calls := seamOnPath(t, "factory-intake", `
if [ "$1" = filed ]; then echo "filed $2 $3" >> $CALLS; exit 0; fi
echo '{"ask":"LYR-200: make it fast","source":"linear:LYR-200","parts":[{"name":"a","repo":"hev/kit","task":"t"}]}'
echo '{"ask":"no source"}'
echo 'not json'`)

	rep := tickT(t)
	if len(rep.Filed) != 1 || len(w.started) != 1 {
		t.Fatalf("first tick: filed %v, gaffers %v", rep.Filed, w.started)
	}
	id := rep.Filed[0]
	j, _ := LoadJob(id)
	if j.Source != "linear:LYR-200" || len(j.Parts) != 1 {
		t.Fatalf("filed job: %+v", j.JobSpec)
	}
	// Printed again next minute (the ack was lost, say): not filed twice,
	// acknowledged again.
	if rep := tickT(t); len(rep.Filed) != 0 {
		t.Fatalf("filed twice: %v", rep.Filed)
	}
	if got := read(calls); strings.Count(got, "filed linear:LYR-200 "+id) != 2 {
		t.Fatalf("acks: %q", got)
	}
	if log := read(filepath.Join(Home(), "intake.log")); !strings.Contains(log, "no source") || !strings.Contains(log, "not a job spec") {
		t.Fatalf("intake.log: %q", log)
	}

	// Once the job is done, the same source is new work again.
	SetJobStatus(id, JobDone, "", "hev")
	if rep := tickT(t); len(rep.Filed) != 1 || rep.Filed[0] == id {
		t.Fatalf("after done: %v", rep.Filed)
	}
}

func TestNotify(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	calls := seamOnPath(t, "factory-notify", `echo "$1 $2 $FACTORY_JOB_SOURCE: $(cat)" >> $CALLS`)
	j, _ := AddJob(JobSpec{Ask: "small", Source: "linear:LYR-7"})

	SetJobStatus(j.ID, JobWaiting, "which region?", "gaffer")
	SetJobStatus(j.ID, JobOpen, "answered", "tick")
	SetJobStatus(j.ID, JobDone, "every part merged", "tick")
	want := "waiting " + j.ID + " linear:LYR-7: which region?\ndone " + j.ID + " linear:LYR-7: every part merged\n"
	if got := read(calls); got != want {
		t.Fatalf("notify calls:\n%q\nwant\n%q", got, want)
	}
}

func TestProgressNotifiesWithoutChangingTheJob(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	calls := seamOnPath(t, "factory-notify", `echo "$1 $2 $FACTORY_JOB_SOURCE: $(cat)" >> $CALLS`)
	j, _ := AddJob(JobSpec{Ask: "small", Source: "linear:LYR-7"})

	got, err := Progress(j.ID, "part api merged: #412", "gaffer x")
	if err != nil {
		t.Fatal(err)
	}
	if got.State.Status != j.State.Status {
		t.Fatalf("status %q, want unchanged %q", got.State.Status, j.State.Status)
	}
	if want := "progress " + j.ID + " linear:LYR-7: part api merged: #412\n"; read(calls) != want {
		t.Fatalf("notify calls %q, want %q", read(calls), want)
	}
	if !strings.Contains(read(filepath.Join(jobDir(j.ID), "log.md")), "Progress: part api merged: #412") {
		t.Fatal("progress not in log.md")
	}
	if _, err := Progress(j.ID, "  ", "gaffer x"); err == nil {
		t.Fatal("empty progress accepted")
	}
}

func TestStuckGafferAlertsOnce(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	newFakeWorld(t)
	calls := seamOnPath(t, "factory-notify", `echo "$1 $2" >> $CALLS`)
	startGafferFn = func(Job, string) (Meta, error) { return Meta{}, errors.New("claude is not installed") }
	j, _ := AddJob(JobSpec{Ask: "small"})

	tickT(t)
	tickT(t)
	if got := read(calls); got != "stuck "+j.ID+"\n" {
		t.Fatalf("stuck alerts: %q", got)
	}
}
