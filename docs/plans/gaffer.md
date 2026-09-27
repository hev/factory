# The gaffer: the always-on host owns every job

## What changes for you

You ask reception on the laptop for something, it files a **job**, and you
close the lid. The job keeps moving without you. A **gaffer** on the always-on
host starts each part, starts the next part when the one it depends on lands,
restarts what died, and tells you on the pull request or the Linear issue when
it needs you. You can also start a job from your phone by moving a Linear
issue to Todo. When you open the laptop again, reception asks the always-on
host what happened. It holds no state of its own.

The names line up with the released OSS factory: reception is the desk you
talk to, and a gaffer is the parent agent for one piece of work. What's
different now is that neither one has a charter. Each is a model with the
`factory` CLI and a job file.

## Decisions (Adam, 2026-09-26)

1. **The always-on host always owns every job.** The laptop never coordinates.
   One owner means no duplicate dispatch and no handing ownership back and forth.
2. **Two ways in.** A reception session files a job, or a Linear issue moves
   into the intake state (Todo by default).
3. **Reception is the client, and the gaffer is the server.**

## Two editions (Adam, 2026-09-26)

Roughly: **open source is one system on one machine, and pro is client/server
with extras.** The gaffer is the same code in both. Only where it runs
changes.

| | Open source | Pro |
|---|---|---|
| Where | One Mac: reception, tick, gaffer and sessions together | Reception on the laptop (client); tick, gaffers and sessions on the always-on host (server) |
| Acts as | Whoever is logged in on that Mac | hevbot on the server, you on the laptop |
| Ways in | Reception | Reception, plus Linear intake |
| Alerts | The dashboard | Plus Slack, through `factory-notify` |
| Memory | Traces | Plus the board, through `factory-brief` |

So "the always-on host" below means the machine the jobs live on. In the open
build, that's the one you're sitting at. The job, the tick and the gaffer must
all work with no second host. Pro adds a remote host, not a different gaffer.
An open question for Adam: does the multi-host part of today's CLI (`hosts
add`, the ssh wire, placement across hosts) stay open source or move to pro?

## The job

A job is a directory on the always-on host, `~/.factory/jobs/<id>/`:

```text
job.md        the ask, in the operator's words or the Linear issue's; parts; done-when
state.json    parts → session ids, PRs, status; blocked-on; waiting-on-you; cursor
log.md        what the gaffer did on each wake, appended
```

- **Parts** are sessions: `{repo, task, after: [part], line?}`. A part runs
  once everything in its `after` list has merged.
- **Done-when** is a shell check (for example `gh pr view 612 --json state`).
  If there isn't one, the job is done when every part's pull request has merged.
- **Ceiling:** wakes and days, each with a default. A job past its ceiling
  stops and asks you. Wakes are the primary limit because they're exact.
  Dollars are advisory only: claude reports a per-turn cost, which is notional
  on a subscription, and codex reports tokens but no dollars.

The job file is the source of truth. The gaffer's context is a cache of it, so
a gaffer can be replaced at any time.

## The gaffer

One gaffer per job. It's an ordinary factory session on the always-on host,
labelled `gaffer`, harness-agnostic, with one difference: it has no repo and
no worktree. Its working directory is the job directory. It gets its brief through `factory-brief`
like any other session. Its prompt is `job.md` plus the new events, and its
tools are the same CLI it runs on that host: `run`, `ls`, `peek`, `send`,
`kill`, and `factory job` to update its own job. Each wake is a `send` to the
gaffer session, so it resumes its own conversation. If that session is lost,
a new gaffer starts from the job file.

**It never merges, and never approves anything on your behalf.** You review
coming out, same as before.

## Waking: a tick with no model

`factory tick` runs every minute under launchd (or `loop d`) on the
always-on host. It's deterministic and calls no model:

1. Collect events since the last cursor. Events come from:
   - fleet, which writes `~/.factory/events.jsonl` when a session is
     `started`, a turn is `turn_done` or `turn_failed` (with its result text),
     or a session is `killed`
   - tick itself, which emits `died` once for a session marked running with
     no runner lock and no tmux session. A session whose host went down can't
     report its own death.
   - pull request state for every part (checks, review, merged), via `gh`
   - replies from you: new comments on the job's pull requests or Linear issue
2. Run intake: `factory-intake` on PATH, if present, prints new job specs as
   JSON lines, and tick files them.
3. For each job with new events, `send` to its gaffer. A send to a busy
   session queues as its next turn, and queued sends fold into one, so no wake
   is lost and none doubles up. If the job has no gaffer, tick starts one.

Sessions never pause to ask. They run headless turns, so a session that needs
you ends its turn and says so in its result. Deciding whether a result is a
question is the gaffer's job, not fleet's.

An idle factory makes no model calls. That's the property the old controller
existed for, kept without the controller.

## Linear intake

Intake is a seam, like `factory-brief`: an executable that prints job specs.
Pro ships `factory-intake-linear`, reading `team` and `state` from
`~/.factory/intake.toml`. The open build ships the seam and no intake. It files a job for each issue that moves into the
intake state, moves the issue to In Progress, and gives the gaffer the issue
to comment on. Linear stays an entry point. It's no longer an approval door:
moving an issue to Todo *is* the ask.

## Talking to you

- **Pull requests and the Linear issue** carry the conversation. The gaffer
  comments there, and your replies come back to it as events.
- **Alerts** (waiting on you, stuck, done, past its ceiling) go out through
  `factory-notify` on PATH, a third seam. Pro wires it to Slack. Without it,
  alerts only show on the dashboard. The board is never used for alerts.

## Reception changes

- `factory job add [--line L] "ASK"` files a job on the host marked `owner`
  in `~/.factory/hosts`, or on the local machine if no host is marked. In pro
  that goes over the same ssh wire as `run`, which is why multi-host is at
  least partly pro. Parts
  are optional: reception can propose them, or leave the split to the gaffer.
- `factory jobs` / `factory job show ID` read state from the always-on host.
- The reception skill stops telling the laptop model to `wait` on sessions.
  It files, then reports what's already moving. `run` stays for quick work
  under your own name on the laptop.

## Out of scope

Lines and images, loops packaging, and the dashboard. The gaffer runs parts
in worktrees until lines exist.

## Build order

1. Events from fleet, plus `factory tick` reading them (no jobs yet; log only).
2. The job directory and `factory job add | show`, and `factory jobs`.
3. The gaffer session and its wakes, plus the `after` ordering and ceilings.
4. `factory-intake` and `factory-notify` seams, plus the Linear intake.
5. The reception skill rewritten as the client.

Each step ships on its own. Check each one against a scratch `FACTORY_HOME`
with a cheap model.
