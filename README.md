# hev factory

Background coding agents on machines you own, coordinated by the model you are
already talking to.

You stay in one Claude session on your laptop. When work is long, parallel, or
should outlive the lid, that session hands it to the factory: each task becomes
its own agent in its own worktree on whichever machine has room, and every
transcript is traced so you can search it afterwards. You never write a plan
file or approve anything in a tracker. You just talk to Claude.

> ## ⚠️ Read this before you run it
>
> **Agents run in yolo mode, as whoever the host is logged in as.** A session is
> `claude --permission-mode bypassPermissions` (or codex with approvals off)
> started by an agent, not by you. It runs shell commands, writes files,
> commits, pushes and opens pull requests with that host's `gh` login, against
> whatever the machine can reach.

## Hosts and who they act as

A factory is a set of hosts. The rule that matters most: **a session acts as
whoever its host is logged in as, never as who asked for it.**

| Host | Acts as | Good for |
|---|---|---|
| An always-on Mac (a mini is the intended shape) | A bot account of its own: its own `gh`, git author, subscriptions and vault | Anything long, heavy, overnight, or run while you are away |
| Your laptop | You | Short parallel work you want under your own name, and overflow when the always-on host is full |

If you want a commit under your own name, run it on your laptop. If you want
it under the bot's, run it on the always-on host. A session can never borrow
the other identity, which is why the bot's host holds none of your logins.
Sessions on a laptop stop when the lid closes, so anything longer than your
attention span belongs on the always-on host.

## The tools

The coordinating model gets a small command-line surface:

```bash
factory hosts                          # each host: identity, live sessions, load, memory, weekly plan use
factory run [--on HOST] REPO "TASK"    # a new session in its own worktree → prints its id
            [--harness claude|codex]
factory ls                             # every session on every host: running, done, failed, died; its PR
factory peek ID                        # the recent transcript
factory send ID "MESSAGE"              # a follow-up, taken as the session's next turn
factory wait ID...                     # block until none of them is running
factory attach ID                      # take over in tmux, then detach and leave it running
factory kill ID [--rm]
factory find "QUERY"                   # search every session's trace, on every host
```

A session is the harness run headless (`claude -p`, `codex exec`) inside tmux
on its host, one turn at a time. The first turn is your task. Each `send`
becomes the next turn, resumed from the harness's own session, so a session
that finished yesterday picks up where it stopped. `attach` swaps the
headless run for the harness's own interface on the same session.

A build can add context to every session's first turn without touching
either harness. If a host has an executable named `factory-brief` on its
PATH, `run` starts it in the new worktree with the task on stdin and
`FACTORY_REPO`, `FACTORY_BRANCH`, `FACTORY_HARNESS`, `FACTORY_HOST` and
`FACTORY_SESSION` set, and puts what it prints into the brief ahead of the
task. It is part of the prompt, so claude and codex read it the same way, and
no hooks or per-harness settings are involved. One that fails, prints
nothing or takes longer than ten seconds adds nothing.

Loops, below, add `factory loops` and `factory loop`.

Without `--on`, `run` places the session itself. It tries the always-on host
first, then spills to other hosts by free cores, free memory and subscription
headroom. It refuses rather than oversubscribe a machine.

## Fan out

Most real work breaks into independent parts: one change per repo, a migration
across twenty call sites, three approaches to compare, a test suite to bisect.
The coordinator should run each part as its own session, spread across the
hosts that have room, and then read the results:

1. Split the work into parts that can each end in their own pull request.
2. `factory hosts` to see where there is room, then one `factory run` per part.
3. `factory ls` and `factory peek` while they run. `factory send` when one
   needs steering. `attach` when you want to steer it yourself.
4. Review each pull request. Use `factory find` to see why a session did what
   it did.

Every session has its own worktree, so ten sessions on one repo do not
collide. None of them waits on another. If one part depends on another, the
coordinator runs it after the first has landed.

## Loops

A loop is a session that runs on a schedule. Loops run on the always-on host,
because a laptop that sleeps misses its schedule. Scheduling is
[hev loop](https://github.com/hev/loop): each loop is a manifest in git, and
every run is recorded with its exit code, its log and a transcript you can
open with `hev trace`.

```bash
factory loops                                  # every loop: schedule, suspended, last run, last exit
factory loop add pr-shepherd --repo OWNER/REPO # install a packaged loop against a repo
factory loop suspend|resume|run NAME
```

Before any model starts, the loop controller checks three things: the
schedule, a cooldown, and a ceiling such as "at most 3 runs a week". A
packaged loop also has a gate, a cheap shell check that exits 0 only when
there is work to do. A loop with nothing to do costs nothing.

The factory ships these loops:

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

Every host runs [hev kit](https://github.com/hev/kit)'s capture daemon into
the same namespace, so a transcript is searchable a few seconds after it is
written, whichever machine wrote it. `factory find` is `hev find` across all of
them, and `hev serve` on the always-on host is the dashboard: sessions, their
traces, and what each one cost.

The board is where agents leave things for the next agent: an investigation's
findings, a credential that turned out to be expired, a port that is always
taken. It holds the agents' notes to each other. Nothing on it is an alert for
you.

## Quick start

```bash
brew install hev/tap/factory
factory hosts add mini        # an ssh alias; repeat for each host
factory hosts                 # confirm each host's identity and headroom
```

Your laptop is always a host and needs no `add`. Each host needs `factory`
itself (the laptop reaches it by running `factory` there over ssh), `claude`
(and `codex`, if you use it) logged in, `gh` logged in as the identity it
should act as, `tmux`, and hev kit's capture daemon (`hev d`) pointed at the
shared namespace. The always-on host also runs the loop controller (`loop d`).

```bash
factory skill install         # teaches your laptop's Claude these tools, as /reception
```

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
