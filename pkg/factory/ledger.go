package factory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultStaleHours is how long a dispatched worker may run with no PR before
// it is worth a look. Override with LEDGER_STALE_HOURS.
const DefaultStaleHours = 4

// Child is one entry in the child ledger — ~/.factory/children/<session>.json,
// written by the gaffer that dispatched the worker. See contracts/child-ledger.md.
// Only the fields a row has room for are decoded; repo, task, brief and the
// rest stay in the file for whoever needs them.
type Child struct {
	Session      string   `json:"session"`
	Instance     string   `json:"instance"`
	RFC          string   `json:"rfc"`
	Plan         string   `json:"plan"`
	Step         string   `json:"step"`
	DispatchedAt flexTime `json:"dispatched_at"`
	PR           *flexInt `json:"pr"`
	// Issue is present only when a person is already involved — machine work
	// never becomes one (contracts/queues.md), so most entries leave it out. It is
	// an identifier rather than a number: issues live in Linear and read
	// `HEV-14`, while older entries carrying a bare GitHub number still parse.
	Issue flexID `json:"issue"`
}

// Tag is the one slug a row has room for: the RFC the work came from, or the
// plan it implements, marked with ~ so the two never read as the same thing.
// With machine work off the issue tracker this is usually the whole answer to
// "what is that worker doing" — the step is in the ledger next to it.
func (c Child) Tag() string {
	switch {
	case c.RFC != "":
		return c.RFC
	case c.Plan != "":
		return "~" + c.Plan
	default:
		return ""
	}
}

// Stale is the "worth a look" signal: dispatched more than threshold ago and
// still no PR. A busy-but-looping worker trips it even while streaming output,
// which is the whole point — a loop looks identical to progress from outside.
func (c Child) Stale(now time.Time, threshold time.Duration) bool {
	if c.PR != nil || c.DispatchedAt.IsZero() {
		return false
	}
	return now.Sub(c.DispatchedAt.Time) > threshold
}

// StaleThreshold reads LEDGER_STALE_HOURS, falling back to DefaultStaleHours.
func StaleThreshold() time.Duration {
	hours := float64(DefaultStaleHours)
	if v := os.Getenv("LEDGER_STALE_HOURS"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed > 0 {
			hours = parsed
		}
	}
	return time.Duration(hours * float64(time.Hour))
}

// LoadLedger reads every <session>.json in dir, keyed by session name. A file
// that will not parse is skipped: a viewer degrades to the naming convention
// rather than refusing to draw.
//
// A ledger file whose tmux session is gone is stale by definition — the caller
// only ever looks up sessions it already knows are live.
func LoadLedger(dir string) map[string]Child {
	out := map[string]Child{}
	if dir == "" {
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var child Child
		if err := json.Unmarshal(data, &child); err != nil {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if child.Session == "" {
			child.Session = name
		}
		out[name] = child
	}
	return out
}

// flexInt accepts a JSON number or a string holding one. Ledger files are
// written by an agent following prose, so "issue": 14 and "issue": "14" both
// turn up in practice.
// flexID is an issue identifier that arrives as either a JSON string
// (`"HEV-14"`) or, from a ledger written before issues moved to Linear, a bare
// number. It keeps the text it was given, because a Linear identifier has no
// numeric form to fall back to.
type flexID string

func (f *flexID) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if text == "null" {
		text = ""
	}
	*f = flexID(text)
	return nil
}

// String is the identifier as the ledger carries it, with no sigil. How it is
// decorated for a screen is the picker's business (internal/picker/rows.go).
func (f flexID) String() string { return string(f) }

type flexInt struct {
	Value int
	Set   bool
}

func (f *flexInt) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if text == "" || text == "null" {
		return nil
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return nil // an unparseable issue number is a blank column, not a crash
	}
	f.Value, f.Set = n, true
	return nil
}

func (f flexInt) String() string {
	if !f.Set {
		return ""
	}
	return strconv.Itoa(f.Value)
}

// flexTime parses the RFC 3339 timestamps the ledger spec calls for, and
// tolerates the space-separated form an agent occasionally writes instead.
type flexTime struct{ time.Time }

func (f *flexTime) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if text == "" || text == "null" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			f.Time = parsed
			return nil
		}
	}
	return nil // no dispatch time means no stale signal, which is the safe default
}
