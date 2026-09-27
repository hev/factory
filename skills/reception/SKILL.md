---
name: reception
description: Hand work to hev factory, background coding agents on machines you own, and coordinate them from this session with the `factory` CLI. Use when work is long, heavy or parallel, should outlive the laptop lid, should be done under the bot identity rather than the user's, or splits into parts that can each end in their own pull request. Also use to check on, steer, take over, kill or search factory sessions ("what's running", "how is the mini doing", "what did that session try"), and when the user opens a session with /reception.
---

# Reception

You are the coordinator. The factory gives you background sessions: each is
its own agent, in its own worktree, on whichever host has room, and its
transcript is traced. You split the work, start the sessions, watch them,
steer them and read their pull requests. Nothing coordinates them but you.

## Who a session acts as

**A session acts as whoever its host is logged in as, never as who asked for
it.** `factory hosts` shows each host's identity. The always-on host is a bot
account with its own `gh` login, git author and subscriptions; the laptop
(`local`) is the user. So the host decides whose name is on the commit and
the pull request:

- Work that should be the bot's (most of it): the always-on host.
- Work the user wants under their own name, or that needs something only the
  laptop has (their logins, a browser, a local-only credential): `--on local`.
  Laptop sessions stop when the lid closes.

If identity matters, pass `--on`. Without it, placement tries the always-on
host first and spills to the laptop, which changes whose name the work goes
out under.

## The tools

```bash
factory hosts                           # identity, live/slots, load, memory, weekly plan use, per host
factory run [--on HOST] REPO "TASK"     # prints the new session's id
            [--harness claude|codex] [--model M] [--base BRANCH]
factory run --on mini hev/lyr - <<'EOF' # a long task from stdin
...
EOF
factory ls [--all] [--json]             # every session on every host: status, PR, task
factory peek ID [-n 80]                 # what it said and did, one line per tool call
factory send ID "MESSAGE"               # a follow-up; it becomes the session's next turn
factory wait ID...                      # block until none of them is running (run it in the background)
factory kill ID [--rm]                  # stop it; --rm also removes the worktree
factory find "QUERY" [--session ID]     # hev kit search over every session's trace
factory attach ID                       # for the user, in their own terminal: see below
```

REPO is `OWNER/REPO`, or `.` for the checkout you are in. Each session gets
branch `factory/<id>`, cut from the default branch unless you pass `--base`.
IDs match on any unique prefix.

A status is one of `running`, `done` (the turn ended; its PR and summary are
ready), `failed` (the harness errored; `peek` shows the stderr), `died` (it
was running when its host went down or its tmux session was killed),
`killed`, or `interactive` (someone attached). `+N` after it means N
follow-ups are queued for the next turn.

## When to hand off

Hand off when the work is longer than a few minutes of tool calls, when it can
run in parallel, when it should keep going after the user leaves, or when it
should be the bot's work. Do it yourself when it is a quick question, a small
edit the user is watching, or anything that needs this conversation's context
turn by turn.

Say what you are doing: "starting three sessions on the mini: …", with their
ids.

## Writing the task

A session starts cold. It has the repo and your task, nothing else from this
conversation, and it cannot ask you anything mid-turn. If it gets stuck it
ends its turn and says so, and you see that in `peek`.

The factory already tells every session its host, identity, worktree and
branch, to open a draft pull request early, never to end a turn waiting on a
background task, and to finish with a summary. Don't repeat those. Write the
rest:

- **The outcome**, in a sentence, and how to tell it is done: the test that
  must pass, the command whose output must change, the page that must render.
- **Everything you know that it would otherwise rediscover**: file paths,
  the cause if you found it, what was already tried, decisions the user made
  in this conversation, links to issues and PRs.
- **The edges**: what not to touch, whether to merge (by default it does
  not), and whom to ask if it is blocked (it can comment on the PR).

A task that fits in one line is fine when the repo says the rest.

## Fan out

1. Split the work into parts that can each end in their own pull request.
   Parts that depend on each other run in order: start the second when the
   first has landed, or tell it to base on the first's branch (`--base`).
2. `factory hosts` to see where there is room, then one `factory run` per
   part. A host refuses rather than oversubscribe; if nothing has room, say
   so and queue the rest yourself.
3. While they run, don't poll by hand. Start `factory wait ID1 ID2 …` as a
   background shell command; it exits when none of them is running and
   prints how each ended, and that is your cue to pick up. In a one-shot run
   (`claude -p`, or anything with no later turn to pick up in), run it in the
   foreground instead, or your turn ends before the sessions do.
4. For each session that finished, `peek` it and read its PR (`gh pr view`,
   `gh pr diff`). If it is not done, `send` it what is missing. Report to the
   user with every PR linked.

## Steering and taking over

- `send` a correction any time. If a turn is running, the message waits and
  becomes the next turn; it does not interrupt. To stop a session going the
  wrong way, `kill` it and start again with a better task.
- A `died` session resumes where it was with `factory send ID "carry on"`.
- `factory attach ID` stops the stream and opens the harness's own TUI on
  that session in tmux, so it needs a real terminal. Don't run it yourself:
  give the user the command, and tell them that detaching leaves the TUI
  running and that `send` then types into it.

## Finding out what happened

`factory find "QUERY"` searches every session's transcript on every host,
through hev kit. Use `--session ID` to stay inside one. Search before you ask
a session to redo an investigation that another session already did.

The board (the `board` skill) is where agents leave notes for each other:
findings, expired credentials, ports that are always taken. Tell a session to
read or post there when that fits its task. Nothing on the board is an alert
for the user, and nothing from it goes to Slack.

## What you don't do

- Merge, approve or close pull requests, unless the user told you to.
- Start sessions under the user's identity (`--on local`) without saying so.
- Leave a stack of `done` sessions: once a PR has merged, `kill --rm` the
  session to free its worktree.

Older front-desk notes may exist under `~/.factory/reception/<name>/`. Treat
them as an archive of past decisions. Nothing here reads or writes them any
more.
