// Package auth answers the question a floor cannot: are the logins the agents
// run on still good, and when do they stop being good.
//
// It matters here because of how a factory fails when one of them lapses. A
// worker whose `gh` token died does not stop — it keeps going, opens no pull
// request, and reads as busy from every signal the picker has. An expired
// Cloudflare token is a deploy step that fails at the end of an hour of work.
// The cost is always paid late, by an agent, in the middle of something, and
// the fix is always thirty seconds at a shell. That asymmetry is the whole
// argument for putting it on a screen.
//
// Two rules keep it safe to run on the refresh path of a picker:
//
//   - **Nothing here goes to the network.** Every answer is a file the login
//     already wrote. The live probes are a separate, opt-in pass (Probe), and
//     even those are bounded — `op whoami` on a headless Mac over SSH has been
//     measured taking minutes.
//   - **Nothing here reads a secret.** Expiry is metadata; the token itself is
//     never decrypted, never printed and never needed. On macOS that means the
//     keychain is probed for the *existence* of an item and never for its
//     password, which is also what keeps it from raising a GUI prompt at a
//     session that has no GUI.
//
// So an honest "present, but I cannot see when it expires from here" is a
// first-class answer rather than a gap. It is the truthful reading for a
// keychain over SSH, and pretending otherwise would be the one failure that
// matters: a screen that says a credential is fine when nobody checked.
package auth

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// State is how much attention one credential wants.
type State int

const (
	// StateUnknown is "it is there, and this machine cannot tell you when it
	// dies". Deliberately its own state rather than folded into OK: the two
	// look identical on a good day and could not be less alike on a bad one.
	StateUnknown State = iota
	StateOK
	StateExpiring
	StateExpired
	StateMissing
)

func (s State) String() string {
	switch s {
	case StateOK:
		return "ok"
	case StateExpiring:
		return "expiring"
	case StateExpired:
		return "expired"
	case StateMissing:
		return "not logged in"
	}
	return "unreadable"
}

// Attention reports whether this is a row somebody should act on. Missing is
// in and Unknown is out: a login that was never done is a fact, and one whose
// expiry cannot be read from here is a limitation of the reader.
func (s State) Attention() bool {
	return s == StateExpiring || s == StateExpired || s == StateMissing
}

// Expiring is how much warning is worth having. A week is long enough to
// notice on a screen you open most days and short enough that the row is not
// permanently amber.
const Expiring = 7 * 24 * time.Hour

// Credential is one login, as this machine can see it.
type Credential struct {
	Name  string // the short name, and what pins an expiry override
	What  string // what it logs into, in words
	State State
	// Expires is zero when nothing on this machine records one. That is the
	// common case for `gh` and for a 1Password service account, and it is why
	// Pinned exists.
	Expires time.Time
	// Refreshable says a refresh token sits beside the expired one, so the
	// next command fixes it without anybody logging in again. It is the
	// difference between an amber row and a row nobody needs to read.
	Refreshable bool
	// Pinned says the expiry did not come off this machine — it was written
	// down by hand in FACTORY_AUTH_EXPIRY, because the credential carries no
	// expiry a reader could find. A 1Password service account is the case
	// this exists for: it has a hard date and the token does not mention it.
	Pinned bool
	Detail string // where it was read, and anything odd about it
	// Source is the file or store the answer came from, for the panel.
	Source string
}

// Attention is a row worth walking towards.
func (c Credential) Attention() bool { return c.State.Attention() }

// checks are run in the order they are declared, which is also the order they
// are drawn: the two agent logins the factory cannot run without, then the
// things the work touches, then the vault everything else is fetched from.
var checks = []func() Credential{
	checkClaude,
	checkCodex,
	checkGH,
	checkCloudflare,
	checkLinear,
	checkBuffer,
	checkOnePassword,
}

// Check reads every credential this machine knows about. All of them are file
// reads, run together, so the whole pass is one disk's worth of latency.
func Check() []Credential {
	out := make([]Credential, len(checks))
	var wg sync.WaitGroup
	for i, check := range checks {
		wg.Add(1)
		go func(i int, check func() Credential) {
			defer wg.Done()
			out[i] = grade(check())
		}(i, check)
	}
	wg.Wait()
	return out
}

// Attention counts the credentials that want somebody, for the floor's header.
func Attention(creds []Credential) int {
	n := 0
	for _, c := range creds {
		if c.Attention() {
			n++
		}
	}
	return n
}

// Soonest is the credential that dies first among those that have a date, so
// the header can name one rather than only counting them.
func Soonest(creds []Credential) (Credential, bool) {
	var withDates []Credential
	for _, c := range creds {
		if !c.Expires.IsZero() {
			withDates = append(withDates, c)
		}
	}
	if len(withDates) == 0 {
		return Credential{}, false
	}
	sort.Slice(withDates, func(i, j int) bool { return withDates[i].Expires.Before(withDates[j].Expires) })
	return withDates[0], true
}

// grade turns a date into a state, and applies any pinned override.
//
// A check may already have decided it is Missing, which is a fact about
// whether a file exists and outranks anything a date would say.
func grade(c Credential) Credential {
	if pinned, ok := pinnedExpiry(c.Name); ok && c.State != StateMissing {
		c.Expires, c.Pinned = pinned, true
	}
	if c.State == StateMissing || c.Expires.IsZero() {
		return c
	}
	// A refreshable credential's own expiry is not news, in either direction.
	// An access token that lapsed at lunchtime is the ordinary resting state
	// of every OAuth login on a machine nobody has touched since, and one
	// expiring on Tuesday will renew itself on Tuesday. The date is still
	// shown — it is the proof the login is real — but it raises nothing, and
	// a screen that went amber every few hours would be a screen people stop
	// reading. What can actually strand a refreshable login is the refresh
	// token behind it, and nothing on this machine records when that dies.
	if c.Refreshable {
		c.State = StateOK
		return c
	}
	switch left := time.Until(c.Expires); {
	case left <= 0:
		c.State = StateExpired
	case left <= Expiring:
		c.State = StateExpiring
	default:
		c.State = StateOK
	}
	return c
}

// pinnedExpiry reads FACTORY_AUTH_EXPIRY, which is how a credential with a
// real deadline and no machine-readable record of it gets onto the screen:
//
//	FACTORY_AUTH_EXPIRY="1password=2026-11-03,buffer=2026-10-15"
//
// A 1Password service-account token is the reason this exists. It expires on a
// date somebody chose in a web UI, the token itself carries no hint of it, and
// the morning it lapses every `op read` on the machine fails at once — which
// is to say every secret every agent needs. That is exactly the deadline worth
// writing down, and exactly the one nothing can derive.
func pinnedExpiry(name string) (time.Time, bool) {
	for _, pair := range strings.Split(os.Getenv("FACTORY_AUTH_EXPIRY"), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), name) {
			continue
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02"} {
			if t, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// home joins a path under the user's home directory, or returns "" when there
// is no home to join it to.
func home(parts ...string) string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(append([]string{dir}, parts...)...)
}

func exists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// tilde shortens a path for a screen, the way the rest of the factory does.
func tilde(path string) string {
	dir, err := os.UserHomeDir()
	if err != nil || dir == "" || !strings.HasPrefix(path, dir) {
		return path
	}
	return "~" + path[len(dir):]
}

// firstEnv returns the first of these variables that is set, and its name.
// Which one is set matters as much as that one is: a static token in the
// environment silently outranks the login in the keychain, and an operator
// staring at a "logged in" keychain while the environment answers every
// request is the confusion this exists to end.
func firstEnv(names ...string) (string, string) {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return name, value
		}
	}
	return "", ""
}
