package ciwatch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeGH struct {
	pr        PR
	checks    []Check
	err       error
	afterHead string
	calls     int
}

func (f *fakeGH) PullRequest(string, int) (PR, error) {
	f.calls++
	p := f.pr
	if f.afterHead != "" && f.calls%2 == 0 {
		p.Head = f.afterHead
	}
	return p, f.err
}
func (f *fakeGH) Checks(string, int) ([]Check, error) { return f.checks, f.err }
func fixture(t *testing.T) (Store, *fakeGH) {
	t.Helper()
	d := t.TempDir()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	g := &fakeGH{pr: PR{Head: "abc", State: "OPEN"}, checks: []Check{{Name: "tests", Bucket: "pending"}}}
	s := Store{Dir: filepath.Join(d, "ci"), LedgerDir: filepath.Join(d, "children"), Instance: "acme", Repos: []string{"acme/api"}, GH: g, Now: func() time.Time { return now }}
	if err := os.Mkdir(s.LedgerDir, 0700); err != nil {
		t.Fatal(err)
	}
	briefPath := filepath.Join(d, "brief.md")
	if err := os.WriteFile(briefPath, []byte("Implement the approved task."), 0600); err != nil {
		t.Fatal(err)
	}
	ledger, err := json.Marshal(map[string]string{"session": "worker-acme-task", "instance": "acme", "repo": "acme/api", "plan": "accepted-plan", "brief": briefPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.LedgerDir, "worker-acme-task.json"), ledger, 0600); err != nil {
		t.Fatal(err)
	}
	return s, g
}
func register(t *testing.T, s Store) Watch {
	t.Helper()
	w, err := s.Wait("worker-acme-task", "acme/api", 1)
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func only(t *testing.T, s Store) Watch {
	t.Helper()
	ws, err := s.List()
	if err != nil || len(ws) != 1 {
		t.Fatalf("watches=%v error=%v", ws, err)
	}
	return ws[0]
}
func TestPendingSurvivesRestartAndReadyRequiresAck(t *testing.T) {
	s, g := fixture(t)
	w := register(t, s)
	twice := register(t, s)
	if twice.ID != w.ID {
		t.Fatal("duplicate registration")
	}
	for i := 0; i < 3; i++ {
		if err := s.Poll(); err != nil {
			t.Fatal(err)
		}
		if only(t, s).Ready() {
			t.Fatal("pending woke a model")
		}
	}
	if err := s.Ack(w.ID); err == nil {
		t.Fatal("acknowledged pending work")
	}
	// Reopen the same durable state as a new process would.
	restarted := s
	g.checks = []Check{{Name: "tests", Bucket: "pass"}}
	if err := restarted.Poll(); err != nil {
		t.Fatal(err)
	}
	done := only(t, restarted)
	if done.State != "passed" || len(done.Ledger) == 0 {
		t.Fatalf("lost completion or brief: %+v", done)
	}
	calls := g.calls
	g.err = errors.New("offline")
	if err := s.Poll(); err != nil {
		t.Fatal(err)
	}
	if g.calls != calls {
		t.Fatal("ready event was polled again")
	}
	if err := s.Ack(w.ID); err != nil {
		t.Fatal(err)
	}
	ws, _ := s.List()
	if len(ws) != 0 {
		t.Fatal("ack did not consume event")
	}
}
func TestScopeAndLedgerValidation(t *testing.T) {
	s, g := fixture(t)
	for _, args := range [][2]string{{"worker-other-task", "acme/api"}, {"worker-acme-task", "other/api"}, {"../worker-acme-task", "acme/api"}} {
		if _, err := s.Wait(args[0], args[1], 1); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if g.calls != 0 {
		t.Fatal("out-of-scope request reached GitHub")
	}
	os.WriteFile(filepath.Join(s.LedgerDir, "worker-acme-task.json"), []byte(`{"session":"worker-other-task","instance":"acme","repo":"acme/api"}`), 0600)
	if _, err := s.Wait("worker-acme-task", "acme/api", 1); err == nil {
		t.Fatal("accepted another worker's ledger")
	}
}
func TestCompletionCases(t *testing.T) {
	for _, tc := range []struct{ name, state, head, bucket, want string }{
		{"green", "OPEN", "abc", "pass", "passed"}, {"failure", "OPEN", "abc", "fail", "failed"},
		{"cancelled", "OPEN", "abc", "cancel", "failed"}, {"new push", "OPEN", "def", "pass", "superseded"},
		{"closed", "CLOSED", "abc", "pass", "closed"}, {"merged", "MERGED", "abc", "pass", "closed"},
		{"unknown check", "OPEN", "abc", "surprise", "waiting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, g := fixture(t)
			register(t, s)
			g.pr = PR{State: tc.state, Head: tc.head}
			g.checks = []Check{{Bucket: tc.bucket}}
			if err := s.Poll(); err != nil {
				t.Fatal(err)
			}
			if got := only(t, s).State; got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}
func TestPushDuringReadCannotReportGreen(t *testing.T) {
	s, g := fixture(t)
	register(t, s)
	g.calls = 0
	g.afterHead = "new-head"
	g.checks = []Check{{Bucket: "pass"}}
	if err := s.Poll(); err != nil {
		t.Fatal(err)
	}
	if only(t, s).State != "superseded" {
		t.Fatal("reported checks for a different head")
	}
}
func TestNoChecksIsNotSuccessAndEventuallyTimesOut(t *testing.T) {
	s, g := fixture(t)
	w := register(t, s)
	g.checks = nil
	if err := s.Poll(); err != nil {
		t.Fatal(err)
	}
	if only(t, s).Ready() {
		t.Fatal("empty checks treated as green")
	}
	s.Now = func() time.Time { return w.Deadline }
	if err := s.Poll(); err != nil {
		t.Fatal(err)
	}
	if only(t, s).State != "timed-out" {
		t.Fatal("wait never expires")
	}
}
func TestReadFailuresAreBoundedAndReset(t *testing.T) {
	s, g := fixture(t)
	register(t, s)
	g.err = errors.New("network down")
	for i := 0; i < 2; i++ {
		s.Poll()
		if only(t, s).Ready() {
			t.Fatal("transient error woke model")
		}
	}
	g.err = nil
	s.Poll()
	if only(t, s).Failures != 0 {
		t.Fatal("failure streak not reset")
	}
	g.err = errors.New("unauthorized")
	for i := 0; i < 3; i++ {
		s.Poll()
	}
	if w := only(t, s); w.State != "unavailable" || w.Error == "" {
		t.Fatalf("missing bounded error: %+v", w)
	}
}
func TestConcurrentRegistrationAndNoStaleAck(t *testing.T) {
	s, _ := fixture(t)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, err := s.Wait("worker-acme-task", "acme/api", 1)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- w.ID
		}()
	}
	wg.Wait()
	close(ids)
	want := only(t, s).ID
	for id := range ids {
		if id != want {
			t.Fatal("duplicate watcher")
		}
	}
	if err := s.Ack("../../escape"); err == nil {
		t.Fatal("path traversal")
	}
}
func TestCorruptStateFailsLoudly(t *testing.T) {
	s, _ := fixture(t)
	w := register(t, s)
	p := filepath.Join(s.Dir, w.ID+".json")
	os.WriteFile(p, []byte("broken"), 0600)
	if err := s.Poll(); err == nil {
		t.Fatal("corrupt queue was ignored")
	}
}
func TestRemovedScopeAndPermissionMode(t *testing.T) {
	s, _ := fixture(t)
	w := register(t, s)
	info, _ := os.Stat(filepath.Join(s.Dir, w.ID+".json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("watch must be private")
	}
	s.Repos = nil
	s.Poll()
	if only(t, s).State != "unavailable" {
		t.Fatal("removed scope was polled")
	}
	var ledger map[string]any
	if err := json.Unmarshal(w.Ledger, &ledger); err != nil || ledger["plan"] != "accepted-plan" || w.Brief != "Implement the approved task." {
		t.Fatal("brief lost")
	}
}

func TestBriefSnapshotSurvivesOriginalRemoval(t *testing.T) {
	s, _ := fixture(t)
	w := register(t, s)
	var ledger map[string]string
	if err := json.Unmarshal(w.Ledger, &ledger); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ledger["brief"]); err != nil {
		t.Fatal(err)
	}
	if only(t, s).Brief != "Implement the approved task." {
		t.Fatal("lost recovery brief")
	}
}
