package picker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A pane says what an agent is doing, but it says it in the agent's words and
// spread over forty lines. The heuristic in pane.go picks the most recent of
// those lines, which is honest and often cryptic: "✻ Brewed for 14s" is proof
// of life and nothing else.
//
// So a small model reads the pane and writes the row's label. It runs on the
// same `claude` the factory already runs on — subscription auth, no key to
// provision, nothing new in `identity/` — and it is deliberately kept off the
// refresh path:
//
//   - **It never blocks a frame.** The call takes seconds; the screen redraws
//     every two. Refreshes read the cache and return, and a summary appears on
//     a later frame or not at all.
//   - **It only runs on change.** No summary is asked for unless the pane's
//     digest moved, and never more often than the cooldown, so a quiet floor
//     costs nothing.
//   - **It survives the picker closing.** The cache is on disk, because the
//     picker is a screen you open for ten seconds. A summary that only existed
//     in memory would be finished computing right as you quit.
//
// Everything about it degrades to the heuristic: no `claude` on PATH, no
// network, a timeout, a rate limit, an unreadable cache. The picker has always
// worked offline and it still does.
const (
	// summaryCooldown is the shortest gap between two calls for one sub-agent.
	// An agent's *state* changes on the order of a minute; its pane changes ten
	// times in that minute, and paying for each of those buys nothing.
	summaryCooldown = 45 * time.Second

	// summaryTimeout gives up on a call the screen has stopped waiting for.
	summaryTimeout = 30 * time.Second

	// summaryConcurrency caps how many run at once, so a floor with twenty
	// sub-agents on it does not open twenty processes.
	summaryConcurrency = 2

	// summaryModel is the cheapest model that can read a pane and write a
	// phrase. Override with FACTORY_SUMMARY_MODEL.
	summaryModel = "claude-haiku-4-5"

	// summaryChars is the width the label is asked to fit. The row gives it
	// whatever the terminal has left, which on most screens is about this.
	summaryChars = 60
)

// Two prompts, because the picker already knows which case it is in. Whether a
// turn is in flight is decided by the pane's own movement (running, in
// pane.go), and a model asked to infer it from a screenshot gets it wrong —
// it reads the last thing an agent said as the state it is in. So the state is
// told, not asked, and the model is left with the question it is actually good
// at: what is this work.
var (
	busyPrompt = fmt.Sprintf(`You are labelling one row of a terminal dashboard that shows what a fleet of
coding agents is doing. Below is the visible pane of one agent. It is running
right now — a turn is in flight.

Reply with ONE line and nothing else: at most %d characters, present tense, no
preamble, no quotes, no trailing period. Name the work it is in the middle of —
the file, the command, the plan step — rather than the fact that it is working.
Never say it is waiting.

--- pane ---
`, summaryChars)

	quietPrompt = fmt.Sprintf(`You are labelling one row of a terminal dashboard that shows what a fleet of
coding agents is doing. Below is the visible pane of one agent. It is not
running: its last turn has finished and nothing is in flight.

Reply with ONE line and nothing else: at most %d characters, no preamble, no
quotes, no trailing period. Say what it finished or what it stopped on.

If it needs a person before anything else can happen — it asked a question, it
hit a permission prompt, it reported a blocker — begin the line with
"waiting: ". Do not use that prefix for an agent that simply finished.

--- pane ---
`, summaryChars)
)

// summaries is the process-wide cache. The picker is one screen in one
// process, so a package-level singleton is the whole lifecycle.
var summaries = &summarizer{entries: map[string]summaryEntry{}}

type summaryEntry struct {
	Digest string    `json:"digest"` // the pane this label describes
	Text   string    `json:"text"`
	At     time.Time `json:"at"`

	inFlight bool      // a call is out for this session right now
	tried    time.Time // last attempt, successful or not — the cooldown clock
}

type summarizer struct {
	mu      sync.Mutex
	entries map[string]summaryEntry
	loaded  bool
	active  bool          // only the live screen summarizes; see start
	slots   chan struct{} // summaryConcurrency deep; nil until first use
}

// start turns summarizing on for the life of a screen. Only the TUI calls it:
// `factory --list` is one process that prints and exits, and a call it starts
// would outlive the process that wanted the answer. The dump gets the
// heuristic, which is what a one-shot read can honestly produce.
func (s *summarizer) start() {
	s.mu.Lock()
	s.active = true
	s.mu.Unlock()
}

// label returns the cached summary for a sub-agent and, when the pane has
// moved since that summary was written, starts a new one in the background.
// It never waits.
func (s *summarizer) label(session, digest string, lines []string, busy bool) string {
	if !summaryEnabled() {
		return ""
	}

	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return ""
	}
	s.load()
	entry := s.entries[session]
	stale := entry.Digest != digest
	ready := !entry.inFlight && time.Since(entry.tried) > summaryCooldown
	if stale && ready {
		entry.inFlight, entry.tried = true, time.Now()
		s.entries[session] = entry
		if s.slots == nil {
			s.slots = make(chan struct{}, summaryConcurrency)
		}
		go s.write(session, digest, lines, busy)
	}
	s.mu.Unlock()

	return entry.Text
}

// write asks the model for one line and records it. A failure is recorded as
// an attempt and nothing else: the previous label stays, the cooldown keeps
// the failure from repeating every frame, and the row falls back to the
// heuristic if there was never a label to keep.
func (s *summarizer) write(session, digest string, lines []string, busy bool) {
	s.slots <- struct{}{}
	defer func() { <-s.slots }()
	defer func() {
		s.mu.Lock()
		entry := s.entries[session]
		entry.inFlight = false
		s.entries[session] = entry
		s.mu.Unlock()
	}()

	text := ask(lines, busy)
	if text == "" {
		return
	}

	s.mu.Lock()
	entry := s.entries[session]
	entry.Digest, entry.Text, entry.At = digest, text, time.Now()
	s.entries[session] = entry
	s.mu.Unlock()

	persist(session, entry)
}

// ask runs the model over a pane and returns the one line it wrote.
func ask(lines []string, busy bool) string {
	transcript := trimTrailing(aboveInputBox(lines))
	if len(transcript) == 0 {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), summaryTimeout)
	defer cancel()

	prompt := quietPrompt
	if busy {
		prompt = busyPrompt
	}
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt+strings.Join(transcript, "\n"),
		"--model", summaryModelName(), "--allowed-tools", "")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	// One line, whatever the model did with the instruction to write one.
	answer := strings.TrimSpace(string(out))
	if cut := strings.IndexByte(answer, '\n'); cut >= 0 {
		answer = strings.TrimSpace(answer[:cut])
	}
	answer = strings.Trim(answer, `"`)
	if len([]rune(answer)) > summaryChars*2 {
		return "" // not a label: the model ignored the shape, so trust it less
	}
	return answer
}

// ── enablement ───────────────────────────────────────────────

var (
	enabledOnce sync.Once
	enabled     bool
)

// summaryEnabled reports whether to summarize at all. It is on when the
// factory's own harness is installed and the operator has not said otherwise,
// because a factory that runs `claude` for everything else can run it for this
// too. FACTORY_NO_SUMMARY=1 turns it off and the picker goes back to reading
// panes with the heuristic alone.
func summaryEnabled() bool {
	enabledOnce.Do(func() {
		if os.Getenv("FACTORY_NO_SUMMARY") != "" {
			return
		}
		_, err := exec.LookPath("claude")
		enabled = err == nil
	})
	return enabled
}

func summaryModelName() string {
	if m := os.Getenv("FACTORY_SUMMARY_MODEL"); m != "" {
		return m
	}
	return summaryModel
}

// ── the disk cache ───────────────────────────────────────────

// summaryDir holds one file per sub-agent, keyed by session name. It is a
// cache and nothing depends on it: deleting it costs one round of calls.
func summaryDir() string {
	if d := os.Getenv("FACTORY_SUMMARY_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".factory", "summaries")
}

func summaryPath(session string) string {
	dir := summaryDir()
	if dir == "" || strings.ContainsAny(session, "/\\") {
		return ""
	}
	return filepath.Join(dir, session+".json")
}

// load reads the cache once per process, and drops the files of sessions that
// are no longer running so the directory maintains itself. Callers hold the
// lock.
func (s *summarizer) load() {
	if s.loaded {
		return
	}
	s.loaded = true

	dir := summaryDir()
	if dir == "" {
		return
	}
	names, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, file := range names {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		session := strings.TrimSuffix(file.Name(), ".json")
		raw, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			continue
		}
		var entry summaryEntry
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		s.entries[session] = entry
	}
}

func persist(session string, entry summaryEntry) {
	path := summaryPath(session)
	if path == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// forget removes the cached labels of sessions that are gone. The picker calls
// it with every live tmux session — not just this factory's — so switching
// factories does not throw away the labels of the one you just left, and a
// worker that finished takes its file with it.
func (s *summarizer) forget(live map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for session, entry := range s.entries {
		if live[session] || entry.inFlight {
			continue
		}
		delete(s.entries, session)
		if path := summaryPath(session); path != "" {
			_ = os.Remove(path)
		}
	}
}
