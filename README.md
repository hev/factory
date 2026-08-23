# hev factory

Stop prompting Claude directly.

hev factory orchestrates a crew of coding agents on a Mac you own: a front desk
that takes the work, a loop that breaks an approved RFC into tasks, and a
worker per task in its own session. It is for work that takes longer than one
session and spans more than one repo — you approve in Linear, and it reports
what needs you.

> ## ⚠️ Read this before you run it
>
> **A factory runs in yolo mode, as you, on your machine.**
>
> A one-shot beat runs `claude --permission-mode bypassPermissions`
> ([`factory-iterate.sh`](factory-iterate.sh)), and the workers it dispatches
> are `claude` sessions started by an agent, not by you. They run shell
> commands, write files, commit, push, and open pull requests with your `gh`
> login, against whatever the machine can reach.
>
> It writes to Linear as you as well: it moves issues through your team's
> workflow, comments, and labels. The one thing it never does is move an issue
> into the state that means approved, because that is how it is told to build
> something.

## Quick start

### 1. Prerequisites

```bash
brew install tmux go gh jq
gh auth login
```

**Linear.** Approving happens there, so a factory does not run without it:

```bash
claude mcp add --transport http --scope user linear https://mcp.linear.app/mcp
```

Then `/mcp` in any `claude` session to authenticate. Your team needs a workflow
state that means *approved*, which on a default Linear board is `Todo`:
`Backlog` already means captured-but-undecided, and `Todo` means
decided-not-started. Setup asks which one you mean and never guesses.

If you already use Linear for something else and this factory is for a
different workspace, register a second server under its own name —
`linear-acme`, same URL — and authenticate that one against the other
workspace. MCP logins are keyed by server name, so both work side by side and
the config names which one this factory uses.

You also need `claude`, logged in. The factory ships no model and never sees
your API keys: reception, the loop, and every worker it dispatches run `claude`
on the subscription you already hold. Every agent is a tmux session, which is
why you can attach to any of them mid-task and take over by typing. Run it on a
Mac that never sleeps — a laptop that sleeps stops the loop mid-beat, and a Mac
mini is the intended shape.

### 2. Clone and build

```bash
git clone https://github.com/hev/factory ~/workspace/factory
cd ~/workspace/factory
./factory list
```

`list` builds the picker and copies it into your `$GOBIN`, so `factory` works
from any directory; on a fresh clone it reports nothing configured, which is
right. Clone this repo *alongside* your work, never inside it. It is generic
machinery, and your own repos and Linear team stay the source of truth for
plans, issues, and pull requests.

### 3. Run the factory

```bash
./factory
```

There is nothing to configure by hand. On a fresh clone this puts reception on
duty in a tmux session and drops you into the conversation, and reception walks
you through your first factory from there.

### 4. In another pane, watch the floor

```bash
factory
```

That is the picker, and it is the screen you leave open: reception, the gaffers,
and every worker they dispatched.

```
acme   ↵ attach   ^x stop one   ·   type to filter

💁 reception      the front desk — ask anything
── sub-agents ──
●  gaffer-acme          acme                          claude  working    ✻ Brewed for 41s
●⚠ worker-acme-index    acme              ~index      claude  working    ⏺ Bash(npm test -- --watch)
○  worker-acme-search   acme     HEV-14   rfc-search  claude  idle 12m   ⏺ Rebased onto main; ready for review.
```

`↵` on any row attaches to that session, so watching becomes steering the
moment you start typing. Most workers carry no issue at all — machine work
never becomes one — so that column is blank except where a person is already
involved.

## Three abstractions

### Reception

[Reception](contracts/reception-charter.md) is the desk you talk to, and talking to it is
how this is meant to be used: bring it a half-formed idea and it argues back
until there is an RFC worth approving. It writes that RFC into Linear for you
and never approves it — it launches no workers, merges nothing, and never sets
the one state that means build this.

It also handles onboarding — the first conversation ends in a factory config —
but the command underneath is there if you would rather skip ahead:

```bash
./factory init --name acme --plans-repo acme/api --repo acme/api --repo acme/docs \
  --linear-team ENG --linear-approved-state Todo \
  --linear-review-state "In Review" --linear-backlog-state Backlog
```

The repos you name become `repo_scope`, the only boundary in the system, so
keep it to the repos one factory is actually about. `--dry-run` renders the
config without writing it, and
[`factories/example.toml`](factories/example.toml) documents every field.

One desk per factory instance, because a desk speaking for four factories is a
switchboard. `factory-acme` is acme's reception; attach with `tmux attach -t
factory-acme` or `↵` on its row, and the conversation survives the process.

### RFCs

An RFC — request for comments — is where you and the agents work on the same
document. It is a Linear issue whose description is the plan, and you argue
about scope in the comments until it is worth building.

Linear is where everything that needs a person lives. GitHub holds branches,
pull requests and CI, none of which is ever waiting on you. That split is what
makes this approvable from a phone, which was always the point: a queue that
stalls because you were not at a desk is friction, not judgment.

An RFC is a work list, not an essay: what changes for the user, why now, how
success is measured, and steps with acceptance conditions specific enough that
an agent cannot fill a gap with a guess. [`plans/README.md`](plans/README.md)
is the house style, including the 120-line budget.

**You approve by moving the issue into one state.** That is the entire signal,
and a comment is not one. Which state is `linear_approved_state`, picked from
your team's own states at `init`. The gaffer then commits the plan to
`plans/active/` on `plans_repo` and builds from there, so a plan on that branch
is still what "approved" means to everything downstream, and writing one in by
hand still works.

Approved intent has one home even when the work has several. `plans_repo` is a
single repo and `plans_branch` a single branch, which is why the gaffer's
watermark is one `<branch> <sha>`: it senses decisions, and a decision lands in
one place. The repos a plan's steps *touch* are `repo_scope`, and progress
across them is counted per merged pull request rather than off the watermark —
a worker pushing to a work repo is work happening, not somebody approving
something.

A comment is how you *change* an RFC — "looks good but drop step 3" gets
applied to the document, and then the issue sits exactly where you left it
until you move it. [`contracts/approvals.md`](contracts/approvals.md) is the mechanism.
Finished plans archive with a dated stamp, so provenance stays greppable.

**The factory works your board, with one exception.** It moves issues to in
progress when workers start and to done when a plan archives, because a board
that does not track the work is not worth reading. It is barred from exactly
one transition: moving anything *into* the approved state. That exclusion is
the whole boundary, and it holds up because Linear lets an integration create
labels but not workflow states — the marker that carries the decision had to be
handed to it.

The rest of your court is on the same board, in your team's own columns rather
than a vocabulary the factory invented: verified work waiting on you goes to
`linear_review_state`, parked ideas to `linear_backlog_state`, both named at
`init`. Anything blocked on a decision gets a `blocked` label, which sits
alongside whatever state the work is in, and carries an `ASK:` comment with the
one question — edited as it sharpens, never reposted.
[`contracts/queues.md`](contracts/queues.md) is the taxonomy and the order the loop
spends your attention in.

### Loops

[The gaffer](contracts/factory-loop.md) is the parent agent for one factory, and its loop
is the beat. Each beat reads the RFCs you approved, breaks them into tasks,
dispatches a worker per task into its own tmux session, checks what came back,
and reports what needs you. Beats are five minutes apart, so that is the wait
between approving something and hearing about it.

Its instructions are [`contracts/factory-loop.md`](contracts/factory-loop.md): one iteration
written out in prose, a file you read and edit. Changing how a factory operates
is a commit, not a setting.

Every beat reads the world fresh, so nothing carries forward to be wrong and
any worker can be killed and replaced mid-task. There is no step four of seven
to be stuck on, only a gap that is either closed or not. Knowledge is the one
exception to that freshness: every worker reads `docs/learnings/` in the repo
it is about to touch before it starts, and leaves at most one behind when it
finishes ([`contracts/learnings.md`](contracts/learnings.md)). A factory whose
four-hundredth worker is as ignorant as its first is forgetful.

Work that crosses repos is the case this shape is for. The gaffer holds one
RFC across every repo in its scope, opens the pull requests in an order that
accounts for what depends on what, and keeps a per-repo concurrency lane so two
agents never collide in the same tree. Each dispatch posts a line reading
`▶ acme dispatched: …` to the Slack channel you named at setup, if you named
one, so the RFC you approved answers back on its own. The first beat after a
fresh boot reports what it found and dispatches nothing, so the beat after it
is the first that builds anything.

How a beat *runs* is a per-factory setting. `runtime = "resident"` keeps a
claude session alive in tmux and lets the agent schedule its own next beat.
`runtime = "one-shot"` moves the loop out of the agent: each beat is a
`claude -p` process that runs an iteration and exits, so liveness becomes an
exit code and a timestamp instead of a guess about what a pane is doing.

## The picker

The picker ([`cmd/factory`](cmd/factory), [`docs/picker.md`](docs/picker.md))
is the front door and the only screen you need.
It lists the sessions **the factory owns**: reception, the gaffers your
`factories/*.toml` name, and the workers they dispatched. Each row carries the
signals that matter for agent work: which are streaming output (`●`), which are
waiting on you (`○`), which factory a session serves, the plan step it is
working, and a `⚠` on any worker running past four hours without a pull
request. Your own shells are not on it, because a list you have to read past is
one you stop reading.

The last column is read out of each pane every two seconds and labelled by
Claude Haiku, so the screen says what every agent is *doing* and not only that
it is up. A session that has stopped to ask you something reads `waiting:`.

Reception is the top row whether or not the desk is on duty. An off-duty desk
says so and `↵` puts it back on — which matters because the installed `factory`
is the picker alone: it opens the screen without booting anything, so a desk
that exited is a desk nothing restarts until the next `./factory`.

`↵` attaches to the highlighted session and `^x` stops one. Typing filters, so
an RFC slug narrows the list to the workers on it, and `esc` goes back to the
factory list when the machine runs more than one. **Stop the line** is the
andon cord and the one control on the screen that touches a running agent: it
sends `TERM` to every agent in this factory and closes those sessions. It
reaches exactly the rows above it, so your own shells keep running.
**Reception is not on the cord**, because the desk is who you ask about a line
that just stopped.

`factory` is on your `PATH` and stays current on its own: every run of
`./factory` copies the binary it just built into `$GOBIN`, so the command you
type from any directory is the code in this checkout. A global install drifting
behind the repo is the bug nobody looks for. `FACTORY_NO_GLOBAL_INSTALL=1`
turns it off. The installed `factory` is the picker and its subcommands, while
the `./factory` here also boots reception and the gaffers first. Both take the
same arguments.

## Running the machine

```bash
./factory list              # what is configured here, what is up, last beat
./factory cleanup <name>    # remove one factory and everything it left behind
./factory stop --all        # the machine-wide sweep
./scripts/factory-health.sh # what is actually beating
```

`cleanup` shows you every path and asks first, then stops the factory's
sessions, removes its child worktrees, and deletes its state under
`~/.factory`, its config, and the watermark in its workspace. It never touches
GitHub, and it leaves a worktree with uncommitted work standing. `cleanup --all`
takes the machine back to a fresh clone, which is how you start the onboarding
conversation over.

`./factory` is idempotent: it starts what is down and leaves what is up alone.
Run it on a timer and a factory survives a reboot.
[`launchd/com.hev.factory.plist`](launchd/com.hev.factory.plist) is the
template, and it runs `./factory --no-picker` every 300 seconds. Fix the path
inside it, copy it to `~/Library/LaunchAgents/`, then:

```bash
launchctl load ~/Library/LaunchAgents/com.hev.factory.plist
```

Five minutes is a deliberate trade: it is how long an approved RFC sits before
anything happens, and every beat that fires is a model invocation, so the pace
is also spend. A gaffer on the one-shot runtime returns the interval it wants
next at the end of every iteration and has to say why, and fires that arrive
early exit immediately, in bash, before any agent starts. On the resident
runtime the interval is a ceiling on how long a dead loop stays dead.

Every config names a `home_host`, and `./factory` skips any gaffer that does
not belong to this machine. Two gaffers against one RFC source produce
duplicate dispatch: two agents on the same task, one of them wasted. A stale
clone on a laptop is the usual way that happens.

## Where it ends, and where it plugs in

This is the whole loop, and it is deliberately one machine, one GitHub account,
one Linear workspace, one harness. Three things it does not ship each have a
known file name that either exists or does not: a runtime that runs a beat
somewhere else, an identity per role instead of one account, and wherever you
want the status block sent. If the file is absent the factory does the local thing and keeps
going. One page: [`contracts/extending.md`](contracts/extending.md).

[`contracts/`](contracts/) is the layer beneath this one, starting with
[`what-is-a-factory.md`](contracts/what-is-a-factory.md), the normative definition
every other document here should be explainable in terms of. Everything in that
directory changes what the machine does when you edit it; nothing else does.
