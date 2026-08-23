# The picker

The front door to a factory: every sub-agent it is running, live, on one
screen, and what each one is doing this second. `./factory` leaves you here,
and most days it is the only screen you need.

```
acme   ↵ attach   ^x stop one   ·   type to filter   ·   esc switches factory

💁 reception      the front desk — ask anything
── sub-agents ──
●  gaffer-acme          acme                       claude  working    ✻ Brewed for 41s
●⚠ worker-acme-index    acme     #15   ~index      codex   working    ⏺ Bash(npm test -- --watch)
○  worker-acme-search   acme     #14   rfc-search  claude  idle 12m   ⏺ Rebased onto main; ready for review.

🚨 stop the line  3 agent(s) in 3 sub-agent(s)
```

The list is deliberately short: what this factory is running, what each one is
doing, and the one control that stops them.

## One screen is one factory

A machine can run several factories, and a screen that mixed them would make
you read the instance column before you could read anything else. So the
picker belongs to one factory at a time. Configure two and it asks which
before it shows anything:

```
  which factory?   ↵ open   ·   1-9 jumps   ·   esc leaves

▸ 1 acme         acme/api                     desk · gaffer · 2 sub-agent(s)
  2 docs         acme/docs                    not running
```

Configure one and there is nothing to choose, so the question is skipped
entirely. `esc` on the floor goes back to this screen rather than quitting,
which is how you cross from one factory to another.

## What counts as the factory's

The picker is one factory's front door, not a general session switcher. It
lists three things and nothing else:

1. **reception** — `factory-<instance>`, this factory's desk, pinned at the
   top (plus the deskless `reception` on a machine with none configured). The
   row is always there, on duty or not: a desk that is down reads `off duty —
   ↵ puts the desk back on`, and `↵` runs `reception-up.sh` and attaches when
   it comes up;
2. **its gaffer** — `gaffer-<instance>`;
3. **its workers** — a session with a child-ledger entry under
   `~/.factory/children/`, or one named `worker-<instance>-<issue>-<slug>` for an
   instance this machine has configured.

Everything else running in tmux is somebody's own work — the other factories'
sub-agents included. A Mac that runs a factory is usually also a Mac somebody
works on, and a screen mixing the two answers neither question well: it is
either a list of what one factory is doing, or a list of every shell you have
open.

The naming-convention rule in (3) is the fallback for a worker whose gaffer
never wrote a ledger file. It is deliberately narrow — the instance has to be
one of yours — because without that check every `<word>-<number>` shell on the
machine would look like a dispatched worker.

## What a row tells you

- **doing** — what the agent is actually up to. A small model reads the pane
  and writes this line (below); until it has, the column shows the last thing
  the agent said about itself, straight out of the pane. Either way it is the
  column that changes while you watch, so it gets whatever width the terminal
  has left.
- **working ● / idle ○** — whether the pane moved since the last refresh, plus
  the spinner an agent draws only while a turn is in flight. It is deliberately
  **not** tmux's `session_activity`, which stops advancing on a session nobody
  is attached to and reports every working sub-agent as idle. An idle worker
  usually has a question.
- **⚠ stale** — a dispatched worker running past `LEDGER_STALE_HOURS`
  (default 4) with **no PR yet**: worth a look, maybe a coach. It rides
  *alongside* the working dot (`●⚠`), so a busy-but-looping worker trips it
  even while streaming.
- **instance · #issue · RFC/plan** — which factory the session serves
  (colour-coded), the GitHub issue it implements, and an RFC slug or `~plan`
  tag when the ledger carries one.
- **agent** — the foreground process in the active pane (`claude`, `codex`,
  `node`, …). `pane_current_command` is unreliable because Claude renames its
  process title to its version, so the agent is read from the child process of
  the pane's shell.

A **green session name** means a client is attached to it right now. The
folder a session is in is not a column any more — the pane is more interesting
than the path — but it is still what the filter matches, so typing part of a
path still finds the row.

The screen refreshes every two seconds: one `ps` call, a handful of tmux
calls, and one `capture-pane` per sub-agent, run together. A worker dispatched
while you are looking at it appears on its own, and the cursor stays on the row
it was on rather than the line number it was on.

Idle time is counted from the last refresh where that pane changed, so a picker
you just opened says plain `idle` rather than a number it has not earned yet.

## Reading the panes with a small model

`✻ Brewed for 14s` is proof of life and nothing else. So the **doing** column is
written by Claude Haiku reading the pane — `claude -p --model claude-haiku-4-5`,
the same harness and the same subscription the factory already runs on, with no
key to provision and nothing new in `identity/`.

```
●  gaffer-acme          acme            claude  working   Rebasing the intake-inbox branch onto main
○  worker-acme-search   acme    #14     claude  idle 3m   waiting: asks which index to rebuild first
```

`waiting:` is the line worth having. An agent that stopped because it finished
and an agent that stopped because it asked you something look identical in a
row of columns, and telling them apart is most of what the screen is for.

Four rules keep it honest and cheap:

- **It is never on the refresh path.** A call takes seconds and the screen
  redraws every two, so refreshes read the cache and return. A label lands on a
  later frame, or it does not.
- **The state is told, not asked.** Whether a turn is in flight is decided by
  the pane's own movement, and the model is given that answer rather than asked
  to infer it — a model reading a screenshot mistakes the last thing an agent
  said for the state it is in, which is how you get `waiting:` on a sub-agent
  that is mid-run.
- **It only runs on change**, at most once per sub-agent per 45 seconds, and
  only while the picker is on screen. A quiet floor costs nothing, and
  `factory --list` never calls it at all.
- **The cache is on disk** (`~/.factory/summaries/`), because this is a screen
  you open for ten seconds. Labels from the last time you looked are there
  instantly, and the refresh happens behind them.

Everything degrades to the pane's own words: no `claude` on `PATH`, no network,
a timeout, a rate limit, a corrupt cache. Set `FACTORY_NO_SUMMARY=1` to turn it
off, or `FACTORY_SUMMARY_MODEL` to spend more on a better label.

### Where the metadata comes from

The instance / issue / tag / `⚠` columns come from the **child ledger** —
`~/.factory/children/<session>.json`, written by the dispatching gaffer (see
[`docs/child-ledger.md`](../docs/child-ledger.md)). The picker reads it as a
lookup over live sessions and degrades gracefully: no ledger file falls back to
parsing `worker-<instance>-<issue>-<slug>` from the session name, which gives instance
and issue but no stale signal.

Reading is network-free: the gaffer, not the picker, stamps PR state into the
ledger. Steering a flagged worker stays plain `attach + type` — the picker
never actuates.

## Stop the line

The last row is the andon cord, named for the one over a Toyota assembly line
that anyone on the floor can pull. It stops **this factory's sub-agents**, and
it reaches exactly the rows above it: every agent in one gets `TERM` so it can
shut down cleanly, and then those sessions are killed. The confirm names each
session, what it is, and how many agents are in it.

Workers go first, then the gaffer — stopping the line from the far end, so
nothing is still being dispatched into as it goes down. `./factory` brings the
gaffer back; it is a halt, not a teardown.

**Reception is not on the cord.** The desk is who you talk to about a line that
just stopped, and a cord that takes out the person you would ask leaves you
looking at a dead machine with nobody to ask. It stays up, and it is still
there when you want to know what happened.

**Your own sessions are not touched.** A Mac that runs a factory is usually also
a Mac somebody works on, and pulling the cord on a stuck gaffer should not close
the editor in the next window. When you do want the machine-wide sweep it is
`picker stop --all`, which signals every `claude`, `codex` and openclaw-gateway
process it can find (leaving the Claude desktop app alone) and kills no sessions
at all. From a shell, `factory stop <name>` narrows it to one factory on a
machine running several.

The row only appears when there is something running to stop. This is the only
control on the screen that does anything to a running agent; everything else
here reads.

There is intentionally **no file-system browser**, no menu of things you might
also want, and no way to start a session that is not a factory's. Opening a
shell is what a shell is for; this screen is what the factory is doing.

## Keys

| Key           | Action                                                   |
|---------------|----------------------------------------------------------|
| `↵` reception | Attach to the desk — putting it back on duty first if it is off |
| `↵` sub-agent | Attach / switch to it                                    |
| `↵` stop the line | Stop this factory's sub-agents: TERM, then close their sessions (with confirm) |
| `^x`          | Stop the highlighted sub-agent (with confirm)            |
| `^r`          | Refresh now, without waiting for the next tick           |
| type          | Filter the list — substring or subsequence, so `a14s` finds `acme-14-search` |
| `esc`         | Clear the filter; again to switch factory, or to leave when there is only one |
| `^c`          | Leave                                                    |

## Layout

```
cmd/factory/main.go         # subcommand dispatch: picker · list · init · stop
pkg/factory/                # instances, child ledger, scope rule, process table
internal/
├── tmuxctl/                # the tmux CLI: list sessions, read panes, attach, kill
├── ui/                     # palette and column arithmetic
├── picker/                 # the picker, the factory chooser, the pane reader
│                           #   and the model that labels what it read
└── stopline/               # the andon cord
```

`pkg/factory/scope.go` holds the rule for what counts as the factory's, and it
is the one file to edit if that answer should change.

`pkg/factory` is public where the rest is `internal/`, and that is deliberate:
instances, the ledger, session naming and secrets are what anything built
*around* a factory has to agree with, and a second copy of those rules is a
second answer to "is this session ours". The picker's own internals are not
that, and stay unimportable.

## Building and installing

Two copies, on purpose.

**In the checkout**, `./factory` builds `bin/picker` and execs it,
skipping the build unless a source file is newer than the binary. It always
uses this local copy, so a checkout you are editing is never shadowed by a
stale install. The binary is not committed — `.gitignore` covers it.

**On your `PATH`** without doing anything: every run of `../factory` copies the
binary it just built into `$GOBIN`, so the global `factory` never falls behind
the checkout it came from. `FACTORY_NO_GLOBAL_INSTALL=1` opts out, and

```bash
go install github.com/hev/factory/cmd/factory@latest
```

still works if you would rather install it yourself.

It is the picker, not the boot sequence: `./factory` in the checkout is the
shell script that puts reception on duty and starts the gaffers, and the
installed `factory` is the screen that shell script leaves you on. The script
passes every argument that is not a boot flag through to this binary, so
`./factory list` and `factory list` are the same command.

```
factory             the picker
factory --list      print the rows once and exit (what the scope rule includes)
factory stop        the andon cord, from a shell
factory stop <name> the same, for one factory on a machine running several
factory stop --all  the machine-wide sweep, when you really mean everything
```

### How an installed binary finds your factory

It cannot look around itself — `~/golang/bin` has no `factories/` above it. So
`./factory` writes the checkout path to `~/.factory/root` on every run, and the
installed binary reads it. Moving the checkout fixes itself on the next boot;
`FACTORY_ROOT` overrides it for pointing one binary at another checkout.

## Wiring

- **From `./factory`:** the picker is what the start command builds and execs
  into.
- **In tmux:** bind it as a popup — `bind f display-popup -E -w 80% -h 70%
  "factory"`. Give it a key of its own if you already have a general session
  switcher on one; this list is a different question.
- **At login:** run it in a loop for non-tmux interactive shells.

## Environment

| Variable                  | Default                | Meaning                          |
|---------------------------|------------------------|----------------------------------|
| `FACTORY_ROOT`            | `~/.factory/root`      | The factory checkout to read `factories/*.toml` from |
| `FACTORY_LEDGER_DIR`      | `~/.factory/children`  | The child ledger                 |
| `LEDGER_STALE_HOURS`      | `4`                    | Hours before a PR-less worker is flagged `⚠` |
| `FACTORY_INSTANCE_COLORS` | derived from the name  | Pin instance accents, e.g. `acme=#89b4fa,docs=#fab387` |
| `FACTORY_NO_SUMMARY`      | unset                  | Set to anything to stop labelling panes with a model |
| `FACTORY_SUMMARY_MODEL`   | `claude-haiku-4-5`     | The model that writes the **doing** column |
| `FACTORY_SUMMARY_DIR`     | `~/.factory/summaries` | Where those labels are cached |

## Dependencies

`tmux` and a Go toolchain to build. Nothing at runtime but `tmux` — the fzf and
gum the shell version shelled out to are gone, which is what makes the live
refresh and the inline confirms possible. `claude` on `PATH` is optional and
buys the **doing** column its labels.
