// Package ciwatch turns CI completion into a durable, model-free handoff.
package ciwatch

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Check struct {
	Name   string `json:"name"`
	Bucket string `json:"bucket"`
	Link   string `json:"link"`
}
type PR struct {
	Head  string `json:"headRefOid"`
	State string `json:"state"`
}
type GitHub interface {
	PullRequest(repo string, pr int) (PR, error)
	Checks(repo string, pr int) ([]Check, error)
}
type Watch struct {
	ID       string          `json:"id"`
	Instance string          `json:"instance"`
	Worker   string          `json:"worker"`
	Repo     string          `json:"repo"`
	PR       int             `json:"pr"`
	Head     string          `json:"head"`
	State    string          `json:"state"`
	Created  time.Time       `json:"created_at"`
	Deadline time.Time       `json:"deadline"`
	Ledger   json.RawMessage `json:"ledger"`
	Brief    string          `json:"brief_text,omitempty"`
	Checks   []Check         `json:"checks,omitempty"`
	Error    string          `json:"error,omitempty"`
	Failures int             `json:"failures,omitempty"`
}

func (w Watch) Ready() bool { return w.State != "waiting" }
func (w Watch) URL() string { return fmt.Sprintf("https://github.com/%s/pull/%d", w.Repo, w.PR) }

type Store struct {
	Dir       string
	LedgerDir string
	Instance  string
	Repos     []string
	GH        GitHub
	Now       func() time.Time
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s Store) scope(repo string) error {
	if !repoPattern.MatchString(repo) {
		return fmt.Errorf("invalid repository %q", repo)
	}
	for _, r := range s.Repos {
		if strings.EqualFold(r, repo) {
			return nil
		}
	}
	return fmt.Errorf("repository %s is outside %s repo_scope", repo, s.Instance)
}

var processLock sync.Mutex

func (s Store) locked(fn func() error) error {
	processLock.Lock()
	defer processLock.Unlock()
	if !namePattern.MatchString(s.Instance) {
		return fmt.Errorf("invalid instance")
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	// Kernel releases this lock on process exit, including a killed tick.
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
func (s Store) read() ([]Watch, error) {
	files, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return []Watch{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Watch{}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Dir, f.Name()))
		if err != nil {
			return nil, err
		}
		var w Watch
		if err = json.Unmarshal(b, &w); err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name(), err)
		}
		if !idPattern.MatchString(w.ID) || f.Name() != w.ID+".json" || w.Instance != s.Instance || w.PR <= 0 || w.Head == "" {
			return nil, fmt.Errorf("invalid CI watch %s", f.Name())
		}
		switch w.State {
		case "waiting", "passed", "failed", "superseded", "closed", "timed-out", "unavailable":
		default:
			return nil, fmt.Errorf("invalid CI state in %s", f.Name())
		}
		if !namePattern.MatchString(w.Worker) || !strings.HasPrefix(w.Worker, "worker-"+s.Instance+"-") || !repoPattern.MatchString(w.Repo) || w.Created.IsZero() || !w.Deadline.After(w.Created) {
			return nil, fmt.Errorf("invalid CI watch identity or deadline in %s", f.Name())
		}
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out, nil
}
func (s Store) List() ([]Watch, error) { return s.read() }
func (s Store) write(w Watch) error {
	b, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".watch-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.Dir, w.ID+".json"))
}

// Wait registers one worker/PR/head. Duplicate registration is idempotent.
// A ready event must be acknowledged before replacing it with another wait.
func (s Store) Wait(worker, repo string, pr int) (Watch, error) {
	var result Watch
	if !namePattern.MatchString(worker) || !strings.HasPrefix(worker, "worker-"+s.Instance+"-") {
		return result, fmt.Errorf("worker must belong to %s", s.Instance)
	}
	if pr <= 0 {
		return result, fmt.Errorf("PR must be positive")
	}
	if err := s.scope(repo); err != nil {
		return result, err
	}
	err := s.locked(func() error {
		p, err := s.GH.PullRequest(repo, pr)
		if err != nil {
			return err
		}
		if p.State != "OPEN" || p.Head == "" {
			return fmt.Errorf("PR is not open with a known head")
		}
		all, err := s.read()
		if err != nil {
			return err
		}
		for _, w := range all {
			if strings.EqualFold(w.Repo, repo) && w.PR == pr {
				if w.Worker == worker && w.Head == p.Head && w.State == "waiting" {
					result = w
					return nil
				}
				return fmt.Errorf("PR already has watch %s (%s); handle and acknowledge it first", w.ID, w.State)
			}
		}
		ledger, err := os.ReadFile(filepath.Join(s.LedgerDir, worker+".json"))
		if err != nil {
			return fmt.Errorf("worker ledger: %w", err)
		}
		var identity struct{ Session, Instance, Repo, Brief string }
		if err = json.Unmarshal(ledger, &identity); err != nil {
			return err
		}
		if identity.Session != worker || identity.Instance != s.Instance || !strings.EqualFold(identity.Repo, repo) {
			return fmt.Errorf("worker ledger does not match CI registration")
		}
		var brief []byte
		if identity.Brief != "" {
			brief, err = os.ReadFile(identity.Brief)
			if err != nil {
				return fmt.Errorf("worker brief: %w", err)
			}
		}
		b := make([]byte, 16)
		if _, err = rand.Read(b); err != nil {
			return err
		}
		result = Watch{ID: hex.EncodeToString(b), Instance: s.Instance, Worker: worker, Repo: repo, PR: pr, Head: p.Head, State: "waiting", Ledger: ledger, Brief: string(brief), Created: s.now(), Deadline: s.now().Add(24 * time.Hour)}
		return s.write(result)
	})
	return result, err
}

// Poll emits no recurring progress events. Only a terminal state becomes ready.
func (s Store) Poll() error {
	return s.locked(func() error {
		all, err := s.read()
		if err != nil {
			return err
		}
		var errs []error
		for _, w := range all {
			if w.Ready() {
				continue
			}
			if err = s.scope(w.Repo); err != nil {
				w.State = "unavailable"
				w.Error = err.Error()
			} else if !s.now().Before(w.Deadline) {
				w.State = "timed-out"
				w.Error = "CI did not finish within 24 hours"
			} else {
				p, e := s.GH.PullRequest(w.Repo, w.PR)
				if e == nil {
					if p.State != "OPEN" {
						w.State = "closed"
					} else if p.Head != w.Head {
						w.State = "superseded"
					} else {
						var checks []Check
						checks, e = s.GH.Checks(w.Repo, w.PR)
						if e == nil {
							// The checks command addresses a PR: protect against a push during it.
							var after PR
							after, e = s.GH.PullRequest(w.Repo, w.PR)
							if e == nil {
								if after.State != "OPEN" {
									w.State = "closed"
								} else if after.Head != w.Head {
									w.State = "superseded"
								} else {
									w.Checks = checks
									w.State = outcome(checks)
								}
							}
						}
					}
				}
				if e != nil {
					w.Failures++
					w.Error = e.Error()
					if w.Failures >= 3 {
						w.State = "unavailable"
					}
				} else {
					w.Failures = 0
					w.Error = ""
				}
			}
			if e := s.write(w); e != nil {
				errs = append(errs, e)
			}
		}
		return errors.Join(errs...)
	})
}
func outcome(checks []Check) string {
	if len(checks) == 0 {
		return "waiting"
	}
	pending := false
	for _, c := range checks {
		switch c.Bucket {
		case "fail", "cancel":
			return "failed"
		case "pass", "skipping":
		default:
			pending = true
		}
	}
	if pending {
		return "waiting"
	}
	return "passed"
}
func (s Store) Ack(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid watch id")
	}
	return s.locked(func() error {
		all, err := s.read()
		if err != nil {
			return err
		}
		for _, w := range all {
			if w.ID == id {
				if !w.Ready() {
					return fmt.Errorf("watch is still waiting")
				}
				return os.Remove(filepath.Join(s.Dir, id+".json"))
			}
		}
		return fmt.Errorf("unknown watch %s", id)
	})
}
