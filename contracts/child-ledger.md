# The child ledger

The factory's structured record of live child sessions — the thing that lets a
glance at the fleet answer *which factory is this, what is it working, and has
it been grinding with nothing to show?* It closes the "no structured child
ledger" duct tape named under the **Workers** organ in
[`what-is-a-factory.md`](what-is-a-factory.md), and it is the data source the
tmux picker reads today and R1 (beat telemetry) / R3 (dashboard) will read next.

## Shape

One JSON file per live child, keyed by its tmux session name, machine-local:

```
~/.factory/children/<session>.json
```

(Override the directory with `FACTORY_LEDGER_DIR`.) Machine-local by design:
children run where their tmux session runs, so the ledger lives there too. The
picker on another machine reads that machine's ledger.

```json
{
  "session": "worker-acme-search-index",
  "instance": "acme",
  "repo": "acme/api",
  "plan": "search-rework",
  "step": "wire turbopuffer credential preflight",
  "brief": "/path/to/brief.md",  // optional
  "rfc": "agent-ready-pr-handoff",  // optional — the RFC's slug (filename minus .md)
  "dispatched_at": "2026-07-10T14:03:00Z",
  "pr": 41,                      // set once the child opens a PR; absent until then
  "issue": "HEV-14",             // optional — only when a human-facing issue exists
  "issue_url": "https://linear.app/acme/issue/HEV-14",
  "pid": 12345                   // optional
}
```

Required: `session`, `instance`, `repo`, `plan`, `dispatched_at`. Everything
else is optional and rendered when present.

**`issue` is an identifier, not a number.** Issues live in Linear, so it is
text — `HEV-14` — and the picker prints it as written. A bare number is the
older GitHub form and still decodes, gaining the `#` on screen that GitHub
leaves off.

**The plan and the step are the identity, not an issue number.** Machine work
never becomes an issue ([`queues.md`](queues.md)), so most entries carry no
`issue` at all; the field is there for the case where a person is already
involved — a `blocked` ASK in Linear the worker is unblocking, say. This file
plus the plan document is the whole record of what is in flight.

## Lifecycle — the parent owns it

The dispatching parent is the only writer (see `factory-loop.md`, steps 3
and 6):

1. **Dispatch** — right after launching the child's tmux session, write its
   `<session>.json` with `dispatched_at` set and `pr` absent. Name the session
   `worker-<instance>-<slug>` so consumers degrade gracefully without the file.
2. **Tending beat** — when the parent detects the child's PR, stamp `pr`. This
   keeps PR-state resolution off the reader's hot path (the picker never calls
   the network), and it is what marks the session reapable once it falls quiet.
3. **Harvest** — `scripts/factory-reap.sh <instance>` writes the pane and this
   file to `~/.factory/harvest/<instance>/<session>.log`, kills the session and
   deletes the entry. It runs every beat from the wrapper and every timer fire
   from `factory-up.sh`, so this happens whether or not the gaffer gets to
   step 6. Entries whose session is already gone are cleared by the same pass.

## How the picker reads it

The picker treats the ledger as a *lookup table* over live tmux sessions:

- **File + `jq` present** → render `instance`, an `RFC`/plan tag, the issue
  identifier if there is one, and
  a `⚠` stale marker when `pr` is absent **and** `now - dispatched_at` exceeds
  `LEDGER_STALE_HOURS` (default 4). That `⚠` is the "worth a look — long-running,
  still no PR" signal; a busy-but-looping child trips it even while streaming.

  A row has width for four of these. The rest — `repo`, `step`, `brief`,
  `issue_url`, `pr`, `dispatched_at` — are what the picker's detail panel shows
  for the one child somebody has the cursor on, which is what makes writing
  them worth the gaffer's trouble. **`repo` and `step` in particular are read
  by a person now**, not just stored: `repo` is printed beside the directory
  the pane is actually in, so a child dispatched against one repo and running
  in another says so.
- **No file, or no `jq`** → fall back to the `worker-<instance>-` prefix, so
  the instance still shows (no plan, no stale signal without `dispatched_at`).
- **Neither** → a non-factory session (your own shells) renders exactly as
  before. Backward-compatible by construction.

Reading is network-free and side-effect-free: the ledger is written by the
parent, never by a viewer. Steering a flagged child stays plain `attach + type`,
per the standing invariant *Actuation only by attachment*.
