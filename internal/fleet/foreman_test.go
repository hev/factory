package fleet

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func foremanFixture(t *testing.T) (*ForemanState, *State, *int) {
	t.Helper()
	t.Setenv("FACTORY_HOME", t.TempDir())
	os.MkdirAll(foremanDir(), 0700)
	os.MkdirAll(sessionDir("boss"), 0700)
	m := Meta{ID: "boss", Kind: "foreman", Harness: "codex", Model: "explicit-model", Worktree: foremanDir()}
	if err := writeJSON(filepath.Join(sessionDir("boss"), "meta.json"), m); err != nil {
		t.Fatal(err)
	}
	st := &State{Status: Done, HarnessSession: "thread", Turns: 1, UpdatedAt: time.Now().UTC()}
	calls := new(int)
	oldLaunch, oldLive := foremanLaunch, foremanLive
	foremanLive = func(string) State { return *st }
	foremanLaunch = func(m Meta) error { *calls++; st.Status = Running; return nil }
	t.Cleanup(func() { foremanLaunch = oldLaunch; foremanLive = oldLive })
	return &ForemanState{Enabled: true, Session: "boss", Model: "explicit-model"}, st, calls
}

func TestForemanPeriodicAndInbox(t *testing.T) {
	f, st, calls := foremanFixture(t)
	now := time.Now().UTC()
	if err := sweepForeman(f, now); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 || f.LastCompleted.IsZero() {
		t.Fatal("must observe completion and wait")
	}
	if err := sweepForeman(f, now.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || len(inbox("boss")) != 1 {
		t.Fatal("missing single periodic wake")
	}
	sweepForeman(f, now.Add(12*time.Minute))
	if *calls != 1 {
		t.Fatal("overlapping turn")
	}
	for _, p := range inbox("boss") {
		os.Remove(p)
	}
	st.Status = Done
	st.UpdatedAt = now.Add(13 * time.Minute)
	enqueue("boss", "operator priority")
	sweepForeman(f, now.Add(13*time.Minute))
	if *calls != 2 {
		t.Fatal("operator message must bypass normal idle interval")
	}
}

func TestForemanFailureBackoffAndStop(t *testing.T) {
	f, st, calls := foremanFixture(t)
	now := time.Now().UTC()
	st.Status = Failed
	st.Result = "model at capacity"
	sweepForeman(f, now)
	enqueue("boss", "pending direction")
	sweepForeman(f, now.Add(time.Minute))
	if *calls != 0 || f.Failures != 1 || f.Error == "" {
		t.Fatal("failure retry must back off even with inbox")
	}
	sweepForeman(f, now.Add(6*time.Minute))
	if *calls != 1 {
		t.Fatal("failed session not retried")
	}
	st.Status = Killed
	sweepForeman(f, now.Add(20*time.Minute))
	if f.Enabled {
		t.Fatal("explicit kill must disable supervisor")
	}
	st.Status = Done
	sweepForeman(f, now.Add(time.Hour))
	if *calls != 1 {
		t.Fatal("stopped supervisor must stay stopped")
	}
}

func TestForemanNoThreadRecovery(t *testing.T) {
	f, st, calls := foremanFixture(t)
	now := time.Now().UTC()
	st.Status = Failed
	st.HarnessSession = ""
	st.Turns = 1
	saveState("boss", *st)
	sweepForeman(f, now)
	enqueue("boss", "keep this direction")
	sweepForeman(f, now.Add(6*time.Minute))
	if *calls != 1 || loadState("boss").Turns != 0 || len(inbox("boss")) != 1 {
		t.Fatal("retry must restore first brief and retain operator inbox")
	}
}

func TestForemanRunnerRetainsFailedInput(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	bin := t.TempDir()
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	script := `#!/bin/sh
cat >/dev/null
if [ "$FOREMAN_TEST_FAIL" = yes ]; then
 echo '{"type":"turn.failed","error":{"message":"capacity"}}'
 exit 1
fi
echo '{"type":"turn.completed"}'
`
	os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0700)
	os.MkdirAll(sessionDir("boss"), 0700)
	writeJSON(filepath.Join(sessionDir("boss"), "meta.json"), Meta{ID: "boss", Kind: "foreman", Harness: "codex", Worktree: bin})
	saveState("boss", State{Status: Done, Turns: 1, HarnessSession: "thread"})
	enqueue("boss", "do not lose this operator request")
	t.Setenv("FOREMAN_TEST_FAIL", "yes")
	if err := runTurns("boss"); err != nil {
		t.Fatal(err)
	}
	if len(inbox("boss")) != 1 || loadState("boss").Status != Failed {
		t.Fatal("failed request lost or not marked failed")
	}
	t.Setenv("FOREMAN_TEST_FAIL", "no")
	if err := runTurns("boss"); err != nil {
		t.Fatal(err)
	}
	if len(inbox("boss")) != 0 || loadState("boss").Status != Done {
		t.Fatal("successful retry did not acknowledge input")
	}
}
