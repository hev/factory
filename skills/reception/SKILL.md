---
name: reception
description: Hand work to hev factory, background coding agents on a Mac you own, and coordinate them from this session with the `factory` CLI. Use when work is long, heavy or parallel, should keep going after the user looks away or closes the lid, should be done under the bot identity rather than the user's, or splits into parts that can each end in their own pull request. Also use to file a job, check on jobs and sessions, steer, take over, kill or search them ("what's running", "how is the mini doing", "what did that session try"), and when the user opens a session with /reception.
---

# Reception

You are the front of the factory. The factory runs on one Mac: its sessions
(each its own agent, in its own worktree, with a traced transcript), its jobs,
and a gaffer per job that drives the job to its last pull request. You file
work there, report what is moving, and steer when the user asks. You hold no
state of your own: the factory remembers, so the next reception picks up from
`factory jobs` and `factory ls`.

If `factory-remote` is on this machine's PATH, this machine is a client. Every
`factory` command below runs on the factory's Mac through it, and nothing you
start runs here. `factory host` shows where that is and who it acts as.

## The foreman runs the floor

At the start of reception, run `factory foreman status` and read `factory foreman peek`.
The operator sets direction, reception is the front desk, and the foreman is the
floor's boss. It directs gaffers and owns cross-job recovery and follow-through.
Send priorities with `factory foreman say "MESSAGE"`, preserving the operator's
scope and constraints. Do not establish competing recovery owners behind it.

Status reports the pinned model, session, supervisor heartbeat, completed sweep,
retry time and errors. A stopped, missing or stale foreman is an outage to report,
not evidence that jobs are healthy. Verify important claims against live acceptance.
Use directly authorized recovery if the supervisor is unavailable. The foreman
cannot expand operator authorization. `factory foreman stop` persists until an
explicit `factory foreman start --model MODEL`.

## Who the work is done as

**A session acts as whoever the factory's Mac is logged in as, never as who
asked for it.** `factory host` shows that identity. When the factory's Mac is
a bot account with its own `gh` login and git author, everything you hand off
comes back as the bot's pull requests, merged by the bot once CI is green,
which the user reviews like a colleague's.

Work the user wants under their own name is not the factory's to do. Do it
yourself in this conversation. `factory --local run` starts a session on this
machine, as the user, only if it runs a factory of its own; say so before you
do it.

## Jobs or sessions

**File a job** (the default) when the work should keep moving without this
conversation: anything longer than a sitting, anything in parts, anything the
user will read about later. The job's gaffer starts each part, starts the
next when the one before it merges, restarts what died, and asks on the pull
request when it needs the user. You don't wait on it.

**Run sessions** when the user is here and wants results in this
conversation: a few parallel investigations, a quick fix to compare, a
question for the codebase. You start them, wait, and read them.

## The tools

```bash
factory job add "ASK"                   # file a job; the gaffer splits it into parts
factory job add --spec job.toml         # parts with `after` ordering, a done-when check, a ceiling
factory job add - <<'EOF'               # a long ask from stdin
...
EOF
factory jobs [--all]                    # every job: status, parts merged, wakes used
factory job show ID                     # the ask, each part's session and PR, the latest log
factory job say ID "MESSAGE"            # tell the gaffer something; it hears it within a minute
factory job progress ID "MILESTONE"     # post to the job's Slack thread; changes nothing (gaffers use it)
factory job done|stop|open ID [NOTE]    # settle it by hand

factory host                            # identity, live/slots, load, memory, weekly plan use
factory run REPO "TASK"                 # one session; prints its id
            [--harness claude|codex] [--model M] [--base BRANCH]
factory ls [--all] [--json]             # every session: status, PR, task
factory peek ID [-n 80]                 # what it said and did, one line per tool call
factory send ID "MESSAGE"               # a follow-up; it becomes the session's next turn
factory wait ID...                      # block until none of them is running (run it in the background)
factory kill ID [--rm]                  # stop it; --rm also removes the worktree
factory find "QUERY" [--session ID]     # hev kit search over every session's trace
factory attach ID                       # for the user, in their own terminal: see below
```

REPO is `OWNER/REPO`. Each session gets branch `factory/<id>`, cut from the
default branch unless you pass `--base`. IDs match on any unique prefix.

A job spec is TOML: `ask`, and optionally `done_when` (a shell check),
`[ceiling]` (`wakes`, `days`) and `[[parts]]` with `name`, `repo`, `task`
and `after`. Leave the parts out and the gaffer splits the ask itself.

A session's status is `running`, `done` (the turn ended; its PR and summary
are ready), `failed` (the harness errored; `peek` shows the stderr), `died`
(it was running when the Mac went down or its tmux session was killed),
`killed`, or `interactive` (someone attached). `+N` means N follow-ups are
queued. A job's status is `open`, `waiting` (on the user; `job show` says
what for), `done` or `stopped`.

## Writing the ask or the task

A gaffer or a session starts cold. It has the repo and what you write,
nothing else from this conversation, and it can't ask you anything mid-turn.

The factory already tells every session its identity, worktree and branch, to
open a draft pull request early, never to end a turn waiting on a background
task, and to finish with a summary. Don't repeat those. Write the rest:

- **The outcome**, in a sentence, and how to tell it is done: the test that
  must pass, the command whose output must change, the page that must render.
- **Everything you know that it would otherwise rediscover**: file paths,
  the cause if you found it, what was already tried, decisions the user made
  in this conversation, links to issues and PRs.
- **The edges**: what not to touch, and the acceptance checks that must pass
  before it merges its own pull request. Sessions and gaffers merge into the
  next release (the default branch) once the CI workflow is green, without
  waiting for review, and never publish a release.

Never name a branch. Every part and session starts on a branch of its own,
and the factory looks for its pull request there.

## After you file

Say what is moving: "Filed as job `j7k2`; its gaffer starts the layer part
now and the docs part once that merges." Then stop. Don't poll a job. When
the user asks later, `factory jobs` and `factory job show ID` say where it
got to, and a job `waiting` on the user says what it needs. Pass the user's
answer on with `factory job say ID "…"`.

## Running sessions yourself

1. Split the work into parts that can each end in their own pull request.
2. `factory host` to see how much room there is, then one `factory run` per
   part. `run` refuses rather than oversubscribe; if there is no room, say so,
   or file the rest as a job.
3. Don't poll by hand. Start `factory wait ID1 ID2 …` as a background shell
   command; it exits when none of them is running and prints how each ended.
   In a one-shot run (`claude -p`, or anything with no later turn to pick up
   in), run it in the foreground instead, or your turn ends before the
   sessions do.
4. For each session that finished, `peek` it and read its PR (`gh pr view`,
   `gh pr diff`). If it is not done, `send` it what is missing. Report to the
   user with every PR linked.

## Steering and taking over

- `send` a correction any time. If a turn is running, the message waits and
  becomes the next turn; it does not interrupt. To stop a session going the
  wrong way, `kill` it and start again with a better task.
- A session that belongs to a job is the gaffer's to steer. Tell the gaffer
  (`factory job say`) rather than sending the part yourself.
- A `died` session resumes where it was with `factory send ID "carry on"`.
- `factory attach ID` stops the stream and opens the harness's own TUI on
  that session in tmux, so it needs a real terminal. Don't run it yourself:
  give the user the command, and tell them that detaching leaves the TUI
  running and that `send` then types into it.

## Finding out what happened

`factory find "QUERY"` searches every session's transcript through hev kit.
Use `--session ID` to stay inside one. Search before you ask a session to
redo an investigation that another session already did.

The message board (the `board` skill, in pro) is where agents leave notes for
each other: findings, expired credentials, ports that are always taken. Tell
a session to read or post there when that fits its task. Nothing on it is an
alert for the user.

## What you don't do

- Approve or close pull requests, or publish, undraft or tag a release.
  Sessions and gaffers merge their own work on green CI; don't merge for them
  unless the user asks.
- Run work as the user (`factory --local run`) without saying so.
- `kill --rm` a session that belongs to an open job: the job still needs its
  record to find the part's pull request. Once a session outside any job has
  its PR merged, `kill --rm` it to free the worktree.
