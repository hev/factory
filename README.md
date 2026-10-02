# hev factory

Background coding agents on a Mac you own, coordinated by the model you are
already talking to.

You stay in one Claude session. When work is long, parallel, or should keep
going after you look away, that session hands it to the factory: each task
becomes its own agent in its own worktree, and every transcript is traced so
you can search it afterwards. You never write a plan
file or approve anything in a tracker. You just talk to Claude.

> ## ⚠️ Read this before you run it
>
> **Agents run in yolo mode, as whoever the host is logged in as.** A session is
> `claude --permission-mode bypassPermissions` (or codex with approvals off)
> started by an agent, not by you. It runs shell commands, writes files,
> commits, pushes and opens pull requests with that host's `gh` login, against
> whatever the machine can reach.

## One machine, and who it acts as

The factory runs on one Mac: the sessions, the jobs, their gaffers and the
tick. The rule that matters most: **a session acts as whoever that Mac is
logged in as, never as who asked for it.** Its `gh` login opens the pull
requests and its git author signs the commits.

A Mac that stays on is the intended shape, because a laptop that sleeps stops
its sessions and misses its tick. Signed in as a bot account of its own, with
its own `gh`, git author and subscriptions, its work lands as a colleague's
pull requests, and it holds none of your logins.

### From another machine

With an executable named `factory-remote` on PATH, `factory` is a client:
every command that acts on sessions or jobs (`run`, `ls`, `peek`, `send`,
`wait`, `attach`, `kill`, `find`, `job`, `jobs`, `tick`, `host`) is handed to
it whole, and it runs the command on the machine that owns the work. `help`,
`version` and `skill` stay local, and `factory --local VERB` runs one command
here instead. A complete `factory-remote` is one line:

```bash
#!/bin/bash
exec ssh $([ -t 0 ] && echo -t) mini "factory $(printf '%q ' "$@")"
```

That is how a laptop works a factory on a mini. Pro provisions it.

## The tools

The coordinating model gets a small command-line surface:

```bash
factory host                           # this machine: identity, live sessions, load, memory, weekly plan use
factory run REPO "TASK"                # a new session in its own worktree → prints its id
            [--harness claude|codex]
factory ls                             # every session: running, done, failed, died; its PR
factory peek ID                        # the recent transcript
factory send ID "MESSAGE"              # a follow-up, taken as the session's next turn
factory wait ID...                     # block until none of them is running
factory attach ID                      # take over in tmux, then detach and leave it running
factory kill ID [--rm]
factory find "QUERY"                   # search every session's trace
```

A session is the harness run headless (`claude -p`, `codex exec`) inside tmux
on the factory's Mac, one turn at a time. The first turn is your task. Each `send`
becomes the next turn, resumed from the harness's own session, so a session
that finished yesterday picks up where it stopped. `attach` swaps the
headless run for the harness's own interface on the same session.

A build can add context to every session's first turn without touching
either harness. If the factory's Mac has an executable named `factory-brief`
on its PATH, `run` starts it in the new worktree with the task on stdin and
`FACTORY_REPO`, `FACTORY_BRANCH`, `FACTORY_HARNESS`, `FACTORY_HOST` and
`FACTORY_SESSION` set, and puts what it prints into the brief ahead of the
task. It is part of the prompt, so claude and codex read it the same way, and
no hooks or per-harness settings are involved. One that fails, prints
nothing or takes longer than ten seconds adds nothing.

Two more executables do the same for jobs:

- **`factory-intake`** is where work comes from besides you. Every tick runs
  it, and each line it prints is a job spec as JSON (`ask`, `source`, and
  optionally `parts`, `done_when`, `line`). A source with a job already open
  or waiting isn't filed again. After filing, tick runs
  `factory-intake filed SOURCE JOB`, so the intake can mark the work taken.
- **`factory-notify`** is how a job gets your attention. It runs as
  `factory-notify KIND JOB`, where KIND is `waiting`, `done`, `stopped` or
  `stuck` (from tick) or `progress` (a gaffer's milestone, from
  `factory job progress`), with the message on stdin and `FACTORY_JOB`,
  `FACTORY_JOB_SOURCE`, `FACTORY_JOB_ASK` and `FACTORY_HOST` set. A
  `curl` to a webhook is a complete one.

Their stderr goes to `~/.factory/intake.log` and each job's `notify.log`.

A session runs on claude unless the run names a harness, or the machine sets
`FACTORY_HARNESS` (and `FACTORY_MODEL`, which goes with it) in its
environment. Gaffers take theirs from `FACTORY_GAFFER_HARNESS` and
`FACTORY_GAFFER_MODEL`. A run that names `--harness` gets that harness's own
default model unless it names `--model` too.

`run` refuses rather than oversubscribe the machine: it wants a free slot
(one per core), load below 90% of the core count, memory to spare, and the
harness's week not spent.

## Fan out

Most real work breaks into independent parts: one change per repo, a migration
across twenty call sites, three approaches to compare, a test suite to bisect.
The coordinator should run each part as its own session, and then read the
results:

1. Split the work into parts that can each end in their own pull request.
2. `factory host` to see how much room there is, then one `factory run` per part.
3. `factory ls` and `factory peek` while they run. `factory send` when one
   needs steering. `attach` when you want to steer it yourself.
4. Each session merges its own pull request into the next release once its CI
   workflow is green and its acceptance checks pass. Read what landed, and
   use `factory find` to see why a session did what it did.

Every session has its own worktree, so ten sessions on one repo do not
collide. None of them waits on another. If one part depends on another, the
coordinator runs it after the first has landed.

## Jobs and the gaffer

For work that should keep moving after you close the lid, file a job instead
of running sessions yourself. The factory takes it from the ask to its last
pull request.

```bash
factory job add --line lyr "Ordered scan with conditional writes; keep COLLATE C"
factory job add --spec job.toml     # parts with `after` ordering, a done-when check, a ceiling
factory jobs                         # every job: status, parts merged, wakes used
factory job show ID                  # the ask, each part's session and PR, the latest log
factory job say ID "use us-east-1"   # tell its gaffer something
factory job progress ID "part api merged: #412"   # a milestone, to the job's Slack thread (the gaffer's)
```

A job is a directory, `~/.factory/jobs/<id>/`. `job.md` holds the ask and
its parts, `state.json` where each part has got to, and `log.md` what happened.

Each job has a **gaffer**: an ordinary session, with no repo, that
coordinates it. The gaffer splits the ask into parts if you didn't, starts each
part once everything it runs `after` has merged, sends a part a follow-up when
its checks fail or review asks for changes, merges a part's pull request into
the next release once its CI workflow is green and its acceptance checks
pass, and says on the pull request when it needs you. It never approves
anything and never publishes a release: you review what landed, and you own
releases.

`factory tick` runs every minute and calls no model. It reads what sessions
did, and each part's pull request (checks, review, comments, merge), and
settles what needs no judgment: a part merged, the next part ready, the job
done (its done-when check passes, or every part has merged), the job past its
ceiling (50 wakes or 7 days unless you set one). Then it wakes the gaffer of
each job where something changed, and only those. An idle factory makes no
model calls.

## Loops

A loop is a session that runs on a schedule. The factory doesn't schedule
anything itself: loops run on [hev loop](https://github.com/hev/loop), a
separate controller, and the factory ships a few loops packaged for it. Each
loop is a manifest in git, and every run is recorded with its exit code, its
log and a transcript you can open with `hev trace`. Run them on a Mac that
stays on, because a laptop that sleeps misses its schedule.

Before any model starts, the loop controller checks three things: the
schedule, a cooldown, and a ceiling such as "at most 3 runs a week". A
packaged loop also has a gate, a cheap shell check that exits 0 only when
there is work to do. A loop with nothing to do costs nothing.

The factory packages these loops:

| Loop | Gate | What a run does |
|---|---|---|
| `pr-shepherd` | An open PR by this identity has failing CI or unanswered review comments | Fixes CI or answers the review, then pushes |
| `triage` | An issue is assigned or labelled for this identity in GitHub or Linear | Takes the issue to a pull request, or asks on the issue if the ask is unclear |
| `deps` | Weekly | Bumps dependencies, runs the tests, opens one PR per repo |
| `flake-hunt` | Nightly, when the suite has changed | Runs the suite repeatedly, then fixes a flaky test or files it |
| `grade` | A session finished since the last run | Grades the transcript against a rubric and posts what went badly to the board |

A packaged loop is a directory in `loops/` containing a manifest template, a
gate and a prompt. A loop of your own is the same three files.

## Tracing and the dashboard

The factory's Mac runs [hev kit](https://github.com/hev/kit)'s capture
daemon, so a transcript is searchable a few seconds after it is written.
Point every machine you work on at the same namespace and `factory find`
(`hev query`) covers your own sessions too. `hev serve` is the dashboard:
sessions, their traces, and what each one cost.

The board is where agents leave things for the next agent: an investigation's
findings, a credential that turned out to be expired, a port that is always
taken. It holds the agents' notes to each other. Nothing on it is an alert for
you.

## Quick start

```bash
brew install hev/tap/factory
factory host                  # confirm who this machine acts as, and its headroom
factory skill install         # teaches this machine's Claude these tools, as /reception
```

The factory's Mac needs `claude` (and `codex`, if you use it) logged in, `gh`
logged in as the identity it should act as, `tmux`, and hev kit's capture
daemon (`hev d`). Run `factory tick` every minute from launchd so jobs move
while nobody is looking. To work it from a laptop, install `factory` there
too, add a `factory-remote`, and run `factory skill install` on the laptop.

After that, ask for work in plain language.

## What happened to the rest

Earlier versions had a front desk, a foreman, per-plan gaffers, an event
controller and a Linear approval door, with about 3,500 lines of contracts
telling models how to coordinate with each other. Given good tools, a model
coordinates better than any charter we wrote. We kept the machines, the
identities, the tracing and the board, and removed everything else. The
old runtime is in this repo's history, before the pivot.

## From source

```bash
git clone https://github.com/hev/factory ~/workspace/factory
cd ~/workspace/factory && go build ./cmd/factory
```

Apache-2.0.

## Floor foreman

`factory foreman start --model MODEL` starts a persistent Codex supervisor,
independent of job ceilings. Run `factory --local foreman watch` under your host
service manager to keep it available after logout/reboot. The watcher holds a
single-instance lock, checks every 15 seconds and schedules a new floor sweep
five minutes after the previous one finishes. It uses no model while idle.

`factory foreman say "direction"` queues an operator request; `status` returns
JSON with model, session, heartbeat, completed sweep, retry time and error;
`peek` reads the transcript. `stop` persists across watcher restarts, and a
later `start --model MODEL` resumes the same session. A direct session kill
also disables periodic wakes. The model stays pinned. Provider errors back off
from five to thirty minutes, preserving failed inbox messages for retry.

Reception routes operating priorities to the foreman. It directs gaffers,
tracks dispatch-only CI/deployments, resolves cross-job dependencies and may
raise automatic ceilings with evidence. Explicit operator stops and task
boundaries remain authoritative. Notes live in `FACTORY_HOME/foreman/notes.md`;
normal factory session records hold the thread, inbox and transcript. A missing
session record is reported for repair rather than silently replacing it.
