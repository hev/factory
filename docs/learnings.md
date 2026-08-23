# Learnings — the part that compounds

Every beat reads the world fresh. That is what keeps the loop honest: no state
carried forward to be wrong, no step four of seven to be stuck on. It is also
why, without this file, beat 400's worker is exactly as ignorant as beat 1's —
it rediscovers the same house conventions, re-hits the same flaky test, and
re-learns that the migration needs the container up.

A **learning** is the one thing allowed to survive a beat: a short document,
written by the agent that hit the problem, read by the next agent before it
starts. Fresh-read applies to *state* — what is open, what is merged, what is
running. Learnings are not state. They are what the factory knows.

## Where they live

**In the repo they are about, at `docs/learnings/`.** Not in this repo, which
is generic machinery, and not in `~/.factory/`, which no one reviews and no
clone shares.

That placement does three things at once: a worker already sitting in the tree
can `grep` them without a second checkout, they travel with the repo to
whatever machine works on it next, and they arrive through the same gate as
every other change — a `[docs]` pull request the operator can read and reject.

Learnings about a repo go in that repo. Learnings about how the *factory*
operates — a dispatch shape that keeps failing, a preflight that should have
existed — go in `plans_repo`, because that is where the factory's own work is
gated.

## The shape

One problem per file, named for the problem, at
`docs/learnings/<area>/<slug>.md`:

```markdown
---
title: Test suite hangs when the fixtures container is already running
date: 2026-08-21
area: api
kind: environment
tags: [docker, test-fixtures, ci]
source: acme/api#412
---

## What happened
`bin/test` hung with no output for the full session. The fixtures container
from a previous worker was still bound to 5432, so the suite's own container
never came up and the client retried forever.

## What didn't work
- `docker compose down` — the container was started outside compose.
- Raising the connect timeout — it made the hang longer, not louder.

## What works
`bin/test-reset` before the suite. It reaps stray fixture containers by label
and is safe to run when there are none.

## How to avoid it
Any brief that runs `bin/test` on this repo starts with `bin/test-reset`.
Two workers in one repo lane will eventually overlap, and this is what that
overlap looks like from inside the second one.
```

Seven frontmatter fields, and that is the whole schema:

| field | required | what it is |
|---|---|---|
| `title` | yes | the problem, as a sentence. Not the fix. |
| `date` | yes | `YYYY-MM-DD`, written. |
| `area` | yes | the part of the repo — matches the directory. Reuse a value the repo already uses rather than coining a synonym. |
| `kind` | yes | `bug`, `convention`, or `environment` (below). |
| `tags` | no | search keywords, lowercase, hyphenated. |
| `source` | yes | the issue or pull request this came out of. Every learning traces to work. |
| `superseded_by` | no | a path to the learning that replaced this one. |

### The three kinds

- **`environment`** — what the machine has to be for work to happen: a service
  that must be up, a credential that must be live, a port that collides, a
  reset script that must run first. **This is the highest-value kind for an
  unattended fleet**, because it is the class of failure that costs a worker
  its whole session twenty minutes in, and the class that repeats verbatim.
  Preflight reads these (`factory-loop.md`, step 2).
- **`convention`** — how this repo does something, where the answer is not
  derivable from the code in the time a worker has: the migration pattern, the
  error-wrapping idiom, which of three test directories is the live one.
- **`bug`** — a defect that was diagnosed and fixed, where the diagnosis was
  the expensive part and would be expensive again.

`kind` decides nothing procedurally except which preflight reads it. It exists
so a worker scanning fifty titles can skip forty of them.

## The `What didn't work` section is the point

Every other section can be reconstructed from the diff. That one cannot, and it
is the section that saves the next agent a session. A learning that omits it
is usually a learning that was not worth writing.

## Who writes one, and when

**A worker writes at most one, at the end of its task**, as part of the same
pull request as its work — not a separate act, not a separate approval. The
brief carries the instruction.

**The gaffer writes one when a factory-level failure repeats**: a preflight
that failed for a reason preflight should have caught, a dispatch shape that
produced the same bounce twice, local infrastructure that went down the same
way again. These go to `plans_repo`.

**Batch them.** At most one consolidated `[docs]` learnings pull request per
repo per beat, the same rule contract edits already follow. A pull request per
learning is how this becomes noise the operator stops reading.

### The bar

> Would this have saved the last agent time?

If no, do not write it. Specifically, none of these is a learning: a
restatement of what the task did, a summary of the diff, something the
`README` already says, or a problem whose whole solution was reading the error
message. A learnings directory that grows every beat is not compounding, it is
littering — the next agent's cost to read it is real, and it is paid on every
dispatch.

## Who reads one, and when

- **Every worker brief** carries a standing instruction to read
  `docs/learnings/` for its area before starting. That is the whole retrieval
  mechanism: a directory, greppable, in the tree the worker is already in.
- **Preflight** (step 2) reads `kind: environment` for the repos it is about
  to dispatch into. This is how preflight gets smarter without anyone editing
  it — the check that a worker had to discover the hard way becomes the check
  that runs before the next dispatch.
- **Reception** reads them when arguing out a plan, so the plan does not
  specify something the factory already knows does not work.

## Decay — the part that has to be built in from the start

A learnings store nobody prunes becomes a liability with good frontmatter.
Three rules keep it honest:

1. **The contract outranks a learning, always.** `factory-loop.md`, this
   repo's docs, and the workspace repo's own `CLAUDE.md` are what an agent
   loads at the moment it acts. A learning that disagrees with one of them is
   not merely stale — it is going to lose in practice while still costing
   everyone the read. Delete it, or fix the contract; never leave the
   disagreement standing.
2. **Superseding beats editing.** When a learning turns out to be wrong rather
   than incomplete, write the new one and add `superseded_by` to the old.
   Provenance stays greppable, which is the same reason archived plans are
   never deleted.
3. **Sweep every backlog-tending cycle.** Check learnings whose `source` is
   closed and whose subject the tree no longer matches. A learning about a file
   that no longer exists is the easy case; a `convention` learning that
   contradicts current code is the one worth catching.

## What this is not

Not a knowledge base, not a wiki, and not a place to put design notes — those
are plans, and plans have their own lifecycle. Not a log; the beat log
(`~/.factory/beats/<instance>.jsonl`) is the log. Not per-agent memory; a
learning that only makes sense to the agent that wrote it is a learning nobody
will read.

One directory of short files about problems that already cost somebody a
session. That is the whole mechanism, and the reason it is a directory of
markdown rather than a service is that a worker can read it without being told
how.
