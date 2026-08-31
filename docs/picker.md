# The picker

The front door to a factory: every sub-agent it is running, live, on one
screen, and what each one is doing this second. `./factory` leaves you here,
and most days it is the only screen you need.

```
acme  ↵ attach  ^d details  ^g tell gaffer  ^x stop one  ·  type to filter    ! 1 in trouble

── sub-agents ──
●  gaffer-acme                 acme                main            claude  working  dispatching the backfill worker
●! worker-acme-index-backfill  acme        ~index  index-rebuild   codex   working  npm test has failed the same way …
○? worker-acme-search          acme  #14   ~search rfc-search      claude  waiting  asks which index to rebuild first

── worker-acme-index-backfill ──────────────────────────────────────────────────
   where   acme/api  ·  index-rebuild (worktree)  ·  ~/work/acme/api-index
   what    ~index  →  wire turbopuffer credential preflight
   since   dispatched 5h ago  ·  no PR yet  ·  past the stale threshold  ·  moving now
   trouble npm test has failed the same way three times
   ⏺ Bash(npm test -- --watch)
   ⎿ 12 passed, 1 failing: index/backfill_test

🚨 stop the line  3 agent(s) in 3 sub-agent(s)
```

The list is deliberately short: what this factory is running, what each one is
doing, and the one control that stops them. The row the cursor is on says the
rest, and streams while you watch it.

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
lists two things and nothing else:

1. **its gaffer** — `gaffer-<instance>`;
2. **its workers** — a session with a child-ledger entry under
   `~/.factory/children/`, or one named `worker-<instance>-<issue>-<slug>` for an
   instance this machine has configured.

Everything else running in tmux is somebody's own work — the other factories'
sub-agents included. A Mac that runs a factory is usually also a Mac somebody
works on, and a screen mixing the two answers neither question well: it is
either a list of what one factory is doing, or a list of every shell you have
open.

The naming-convention rule in (2) is the fallback for a worker whose gaffer
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
- **the mark** — the one cell that answers *does this need me?*, riding
  alongside the working dot. Three things can claim it and they are ranked by
  what you would do about them:

  | Mark | Means | What it is |
  |------|-------|------------|
  | `!` | rescue it | the model reading the pane says this is going badly |
  | `⚠` | look at it | dispatched past `LEDGER_STALE_HOURS` (default 4) with **no PR yet** |
  | `?` | answer it | the model says it stopped on something only a person can settle |

  `⚠` is the ledger's arithmetic and rides alongside a working dot, so a
  busy-but-looping worker trips it even while streaming. `!` and `?` are read
  out of the words in the pane (below). Only one fits on a row; the detail
  panel carries all three.
- **instance · #issue · RFC/plan** — which factory the session serves
  (colour-coded), the GitHub issue it implements, and an RFC slug or `~plan`
  tag when the ledger carries one.
- **branch** — which branch the pane's directory is on, and the first half of
  *where is this happening*. Read out of `.git` rather than out of `git`: a
  worktree's `.git` is a file pointing at the git directory that holds its own
  HEAD, which is how two workers of one factory sit on two branches of one
  repo. The repo and the path are in the panel next to it.
- **agent** — the foreground process in the active pane (`claude`, `codex`,
  `node`, …). `pane_current_command` is unreliable because Claude renames its
  process title to its version, so the agent is read from the child process of
  the pane's shell.

A **green session name** means a client is attached to it right now.

**Which columns are on screen is decided per screenful.** Widths are still
fixed by hand so the eye can travel down a column, but a column that is empty
on *every* visible row is alignment nobody is using — and on a factory doing
machine work that is usually the issue column, because machine work never
becomes an issue ([`queues.md`](../contracts/queues.md)). The name and branch
columns size to their longest entry; the rest are in or out for the whole
screen, so it stays a grid rather than becoming a row of different shapes.

On a narrow terminal the columns whose content the panel repeats in full are
the ones that go — branch, then tag, then issue, then the name column gives
back what it took. The pane's own words are the last thing to be squeezed and
the last thing to disappear.

The path a session is in is not a column, but it is still what the filter
matches, so typing part of a path still finds the row — as do the repo and the
plan step, which are matched but not printed.

The screen runs on two clocks. **The floor refreshes every two seconds**: one
`ps` call, a handful of tmux calls, and one `capture-pane` per sub-agent, run
together. A worker dispatched while you are looking at it appears on its own,
and the cursor stays on the row it was on rather than the line number it was on.

**The row under the cursor refreshes every third of a second.** Two seconds is
the right price for twenty sessions and the wrong one for the single pane
somebody is actually looking at: one row costs one tmux call, and a screen
meant to show work happening should show it happening. The focused pane feeds
both the panel's transcript and the row's own dot, so moving the cursor onto a
worker starts it streaming.

A label the model wrote is left alone by that fast path. It describes a pane
*state* rather than a frame, and swapping it for the raw last line three times
a second would flicker between two different kinds of answer.

Idle time is counted from the last refresh where that pane changed, so a picker
you just opened says plain `idle` rather than a number it has not earned yet.

## Reading the panes with a small model

`✻ Brewed for 14s` is proof of life and nothing else. So the **doing** column is
written by Claude Haiku reading the pane — `claude -p --model claude-haiku-4-5`,
the same harness and the same subscription the factory already runs on, with no
key to provision and nothing new in `identity/`.

```
●  gaffer-acme          acme    claude  working   Rebasing the intake-inbox branch onto main
○? worker-acme-search   acme    claude  waiting   asks which index to rebuild first
●! worker-acme-index    acme    codex   working   npm test has failed the same way three times
```

It is asked for two things, and answers with both on one line: a **verdict**
and a phrase. The verdict is one of `ok`, `waiting` or `trouble`, and it is
what turns a row red.

`waiting` is the one the screen was built for. An agent that stopped because it
finished and an agent that stopped because it asked you something look
identical in a row of columns, and telling them apart is most of what this is
for.

`trouble` is the one nothing else on the machine can produce. **A worker that
has run the same failing command four times reads as healthy from every
mechanical signal the picker has** — the pane moves, the spinner turns, the dot
is green, the status says `working`. `⚠ stale` catches the version of this that
has been going on for four hours; a model reading the words catches it in the
first minute, and catches the ones that never trip the stale rule at all. The
prompt is explicit that slow, long or large work is `ok`: only a loop with no
progress in it, or a stop it cannot get past, is trouble.

The count of both is drawn in the header, because a floor big enough to scroll
is a floor where the one red row is off screen — and an alarm you have to
scroll to is not an alarm.

**A running agent is never `waiting`.** The pane's own movement already answers
that, by measurement rather than inference, so a model that says otherwise is
overruled rather than believed. Same rule that made the state told-not-asked in
the first place.

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
  only while somebody is attached to the tmux session the picker is running
  in. A quiet floor costs nothing, a picker left running in a session you
  detached from costs nothing, and `factory --list` never calls it at all.
- **The cache is on disk** (`~/.factory/summaries/`), because this is a screen
  you open for ten seconds. Labels from the last time you looked are there
  instantly, and the refresh happens behind them.

Everything degrades to the pane's own words: no `claude` on `PATH`, no network,
a timeout, a rate limit, a corrupt cache. A row with no verdict simply has no
mark, which is the same screen the picker drew before any of this existed. Set
`FACTORY_NO_SUMMARY=1` to turn it off, or `FACTORY_SUMMARY_MODEL` to spend more
on a better label.

The cache outlives the prompt that wrote it. An entry from before verdicts
existed is decoded on the way out rather than thrown away — including the old
`waiting: ` prefix, which becomes the verdict it always meant.

## The detail panel

`^d` toggles it, and it opens on the row the cursor is already on. It is open
by default: the detail is what this screen is for, and a panel somebody has to
know about before they see it is a panel most people never see.

```
── worker-acme-index-backfill ──────────────────────────────────────────────────
   where   acme/api  ·  index-rebuild (worktree)  ·  ~/work/acme/api-index
   what    ~index  →  wire turbopuffer credential preflight
   since   dispatched 5h ago  ·  no PR yet  ·  past the stale threshold  ·  moving now
   trouble npm test has failed the same way three times
   links   https://linear.app/acme/issue/HEV-14
   ⏺ Bash(npm test -- --watch)
   ⎿ 12 passed, 1 failing: index/backfill_test
   ✻ Retrying…
```

A row is about seventy cells of which the pane's own words want fifty.
Meanwhile the child ledger has been carrying the repo, the plan step, the brief
and the pull request all along, and the pane has been carrying the directory
the agent is actually sitting in. The screen answered *what is running*; the
question underneath it — *what is that one doing, and where* — took an attach.

- **where** — the repo the gaffer dispatched against, the branch, and the
  directory the pane is in. Both are shown because they are different kinds of
  fact: the repo is what was *written down*, the path is what is *true*. They
  agree almost always, and the time they do not is the bug that costs an
  afternoon.
- **what** — the plan or RFC, and the step. From the ledger, not the pane: a
  different question from what it is doing this second, with a different
  source.
- **since** — how long it has been out, whether it has a PR to show for it, how
  long the pane has been still, and whether you are attached to it.
- **the verdict** — a line of its own, but only when it is `waiting` or
  `trouble`. `ok` is the state of most of the floor most of the time, and
  saying so on every row is how a screen teaches people to stop reading it.
- **links** — the issue URL and the brief, printed whole, because the only use
  for them is to be copied out of the terminal.
- **the transcript** — the agent's own last few lines, from the fast focus
  read. This is the part that streams.

The panel gives up its transcript before it gives up existing, and gives up
existing before it pushes the list off the screen: a terminal too short for
even one line of pane gets the list it came for.

The andon cord gets a panel where it names every session the cord would reach
before you commit to the keystroke that asks.

## Telling the gaffer

`^g` on a sub-agent writes one line to that factory's gaffer.

Watching the floor is how you find out a worker is stuck, and until this the
only things to do about it were to attach and steer it yourself or to stop it.
Neither is usually what you want: the gaffer dispatched that worker, holds the
plan it came from, and is the thing that should decide whether to coach it,
re-dispatch it, or leave it alone. **Telling the gaffer is a smaller act than
taking the work off it.**

```
⚑ tell gaffer-acme about worker-acme-index-backfill — it reads this on its next beat   ·   ↵ send   ·   esc cancel
⚑ npm test has failed the same way three times▏
```

The line opens pre-filled with the model's own sentence whenever the row is
marked `!` or `?`, so the common gesture is `^g ↵` and you type only when you
know something it does not. What you send goes first — it is the only part the
gaffer could not have worked out for itself — followed by the session, the
where, the what, the state and the reading, so nobody has to go and look the
worker up.

It goes down the rail that already exists:
[`scripts/gaffer-msg.sh`](../scripts/gaffer-msg.sh) writes a JSON file into
`~/.factory/inbox/<instance>/`, which the gaffer drains as step 0 of every
beat. The picker runs the script rather than writing the file so a message the
picker delivers and a message reception delivers have the same representation.
The message says
`from: operator`, because a gaffer weighs a line from the person who owns the
factory differently from one the desk is relaying.

**Priority is always `steer`** — read on the next beat, never an interrupt.
`interrupt` is documented as relaying an order whose value expires before then,
which is a judgement about urgency belonging to whoever is talking to the
operator; that is reception, not a list. This screen's immediate controls stop
a worker. This one tells somebody about it, and says so on the prompt so nobody
stands watching a row that was never going to change.

It is the second thing on this screen that acts rather than reads, and the only
one that does not stop anything.

### Where the metadata comes from

Three sources, and which one a thing came from is worth knowing when they
disagree.

**The child ledger** — `~/.factory/children/<session>.json`, written by the
dispatching gaffer (see
[`../contracts/child-ledger.md`](../contracts/child-ledger.md)) — is where the
instance, issue, tag and `⚠` on the row come from, and the repo, plan step,
brief, issue URL, PR and dispatch time in the panel. Every field the contract
defines is decoded now; the row shows the four it has room for. The picker
reads it as a lookup over live sessions and degrades gracefully: no ledger file
falls back to parsing `worker-<instance>-<issue>-<slug>` from the session name,
which gives instance and issue but no stale signal, and a panel that says so
rather than one full of blanks it cannot explain.

**The pane** gives the directory, and `.git` under it gives the branch. This is
the only part of *where* that is true by observation rather than by report,
which is why the panel prints it alongside the repo instead of instead of it.

**The model** gives the verdict and the label, from the words in the pane.

Reading is network-free: the gaffer, not the picker, stamps PR state into the
ledger, and the branch is a file read rather than a `git` call — the floor
re-reads every two seconds and a subprocess per row is a cost a screen
eventually feels. Steering a flagged worker stays plain `attach + type`. The
picker still never actuates on an agent; `^g` writes to the gaffer's inbox,
which is a message, not an actuation.

## Stop the line

The last row is the andon cord, named for the one over a Toyota assembly line
that anyone on the floor can pull. It stops **this factory's sub-agents**, and
it reaches exactly the rows above it: every agent in one gets `TERM` so it can
shut down cleanly, and then those sessions are killed. The confirm names each
session, what it is, and how many agents are in it.

Workers go first, then the gaffer — stopping the line from the far end, so
nothing is still being dispatched into as it goes down. It is a halt, not a
teardown: `factory up` brings the gaffer back.

**A stop stays stopped.** The boot fire runs every 300s and exists to restart
anything that is down, so a pull that only killed sessions would be undone
before you had finished reading it. Stopping therefore writes a hold file per
factory (`~/.factory/holds/<instance>`), and the boot skips any factory that
has one, saying so on each fire. `factory up` lifts the holds and boots. The
unit is one factory rather than the machine, so one gaffer can be held while
another keeps running, and a reboot with a hold in place still comes up
quiet.

**Your own sessions are not touched, and there is no flag that changes that.**
A Mac that runs a factory is usually also a Mac somebody works on, and pulling
the cord on a stuck gaffer should not close the editor in the next window. The
cord reaches the gaffers, and only the gaffers; an agent the factory never
started is not its to stop, however much it looks like one from the outside.

**Workers keep running.** Stopping the gaffers is what stops the line, because
a gaffer is the only thing that dispatches work — with them down, nothing new
is started and the factory makes no further decisions. A worker is somebody's
task mid-flight, usually a branch with uncommitted work on it, and halting the
line is not a reason to lose a dozen of those. A worker you do want gone is one
row and one `^x`. From a shell, `factory stop <name>` narrows it to one factory
on a machine running several; there is no `--all`, because the cord has one
reach.

The row only appears when there is something running to stop. This is the only
control on the screen that does anything *to* a running agent — `^g` tells the
gaffer about one and stops nothing, and everything else here reads.

There is intentionally **no file-system browser**, no menu of things you might
also want, and no way to start a session that is not a factory's. Opening a
shell is what a shell is for; this screen is what the factory is doing.

## Keys

| Key           | Action                                                   |
|---------------|----------------------------------------------------------|
| `↵` sub-agent | Attach / switch to it                                    |
| `↵` stop the line | Stop this factory's sub-agents: TERM, then close their sessions (with confirm) |
| `^d`, `→`, `←` | Open / close the detail panel on the highlighted row     |
| `^g`          | Tell the gaffer about the highlighted sub-agent           |
| `^x`          | Stop the highlighted sub-agent (with confirm)            |
| `^r`          | Refresh the whole floor now, without waiting for the next tick |
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
├── picker/                 # the picker, the factory chooser, the pane reader,
│                           #   the model that labels and grades what it read,
│                           #   the detail panel and the column arithmetic
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
shell script that installs reception and starts the gaffers, and the
installed `factory` is the screen that shell script leaves you on. The script
passes every argument that is not a boot flag through to this binary, so
`./factory list` and `factory list` are the same command.

```
factory             the picker
factory --list      print the rows once and exit (what the scope rule includes)
factory up          bring up every gaffer registered on this machine, then
                    report one line per factory and a ready count
factory up <name>   the same, for one factory on a machine running several
factory stop        the andon cord, from a shell: the gaffers, held down until
                    the next factory up. Workers keep running
factory stop <name> the same, for one factory on a machine running several
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
