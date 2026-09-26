// Package fleet is the factory: background agent sessions on machines you
// own, each in its own worktree, each acting as whoever its host is logged in
// as.
//
// Everything a session is lives in one directory on its host:
//
//	~/.factory/sessions/<id>/
//	  meta.json     what was asked, where, by which harness (written once)
//	  state.json    where it has got to (rewritten by the runner)
//	  prompt.md     the first turn's input, brief included
//	  log.jsonl     the harness's event stream, every turn appended
//	  inbox/        follow-ups waiting for the next turn
//	  work/         the worktree
//
// The directory is the whole record. There is no database and no daemon: a
// session in flight is a tmux session running `factory _runner <id>`, and a
// session at rest is these files.
package fleet

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Status values. running and interactive are live; the rest are at rest.
const (
	Running     = "running"     // the runner is driving a turn, or about to
	Interactive = "interactive" // someone attached; a TUI owns the session
	Done        = "done"        // the last turn ended cleanly
	Failed      = "failed"      // the last turn ended in an error
	Killed      = "killed"      // factory kill
	Died        = "died"        // said it was running, but nothing is
)

// Meta is what was asked for. Written once, at start.
type Meta struct {
	ID        string    `json:"id"`
	Host      string    `json:"host"`
	Repo      string    `json:"repo"`
	Base      string    `json:"base"`
	Branch    string    `json:"branch"`
	Worktree  string    `json:"worktree"`
	Harness   string    `json:"harness"`
	Model     string    `json:"model,omitempty"`
	Task      string    `json:"task"`
	Login     string    `json:"login,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// State is where a session has got to.
type State struct {
	Status         string    `json:"status"`
	HarnessSession string    `json:"harness_session,omitempty"`
	Turns          int       `json:"turns"`
	Exit           int       `json:"exit"`
	Result         string    `json:"result,omitempty"`
	CostUSD        float64   `json:"cost_usd,omitempty"`
	PR             *PR       `json:"pr,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PR is the pull request opened from a session's branch.
type PR struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"` // OPEN, MERGED, CLOSED
}

// Session is one row: what was asked and where it has got to.
type Session struct {
	Meta
	State
	Queued int `json:"queued,omitempty"` // follow-ups waiting in the inbox
}

// Home is the factory's state directory on this machine.
func Home() string {
	if dir := os.Getenv("FACTORY_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".factory")
}

func sessionsDir() string         { return filepath.Join(Home(), "sessions") }
func sessionDir(id string) string { return filepath.Join(sessionsDir(), id) }

// NewID is six characters of base32, lower case: short enough to type, and
// 2^30 of them is enough that two hosts never mint the same one.
func NewID() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b[:])
}

// TmuxName is the tmux session an attached session's TUI runs in. Runners
// get a name of their own each time one starts (runnerName), so a runner on
// its way out and one on its way in never contend for a name; the runner
// lock decides which of them works.
func TmuxName(id string) string { return "factory-" + id }

func runnerName(id string) string {
	return fmt.Sprintf("%s-r%x", TmuxName(id), time.Now().UnixNano()&0xffffff)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// writeJSON replaces a file atomically, so a reader never sees half a state.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadMeta(id string) (Meta, error) {
	var m Meta
	err := readJSON(filepath.Join(sessionDir(id), "meta.json"), &m)
	return m, err
}

func loadState(id string) State {
	var s State
	_ = readJSON(filepath.Join(sessionDir(id), "state.json"), &s)
	return s
}

func saveState(id string, s State) error {
	s.UpdatedAt = time.Now().UTC()
	return writeJSON(filepath.Join(sessionDir(id), "state.json"), s)
}

// updateState is a read-modify-write under the state lock, so the runner and
// a `kill` or `ls` never overwrite each other's fields.
func updateState(id string, fn func(*State)) (State, error) {
	unlock, err := flock(filepath.Join(sessionDir(id), "state.lock"), true)
	if err != nil {
		return State{}, err
	}
	defer unlock()
	s := loadState(id)
	fn(&s)
	return s, saveState(id, s)
}

// Resolve finds a session on this host by id or unique id prefix.
func Resolve(prefix string) (string, error) {
	entries, err := os.ReadDir(sessionsDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var hits []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			if e.Name() == prefix {
				return prefix, nil
			}
			hits = append(hits, e.Name())
		}
	}
	switch len(hits) {
	case 0:
		return "", ErrNoSession
	case 1:
		return hits[0], nil
	default:
		sort.Strings(hits)
		return "", fmt.Errorf("%q matches %s", prefix, strings.Join(hits, ", "))
	}
}

// ErrNoSession is a session id this host has never heard of.
var ErrNoSession = errors.New("no such session")

// flock takes an advisory lock on path. With wait false it returns
// errLocked instead of blocking.
func flock(path string, wait bool) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLocked
		}
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

var errLocked = errors.New("locked")

// runnerAlive reports whether a runner holds this session's runner lock. The
// lock, not the tmux session, is the truth: tmux outlives a runner by the
// moment it takes the pane to close, and a runner can be started by hand.
func runnerAlive(id string) bool {
	unlock, err := flock(filepath.Join(sessionDir(id), "runner.lock"), false)
	if err != nil {
		return errors.Is(err, errLocked)
	}
	unlock()
	return false
}

// inbox lists queued follow-ups, oldest first.
func inbox(id string) []string {
	dir := filepath.Join(sessionDir(id), "inbox")
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func enqueue(id, message string) error {
	dir := filepath.Join(sessionDir(id), "inbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%020d.md", time.Now().UnixNano())
	return os.WriteFile(filepath.Join(dir, name), []byte(message), 0o644)
}
