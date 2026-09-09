# The event spool

What the floor says, in words, while it is still true.

Before this existed a worker had no voice. Its state could only be inferred —
a pane snapshot, a child-ledger entry, a harvest log after the fact — so *why
has that one been quiet for an hour* was answered by reading pixels and
guessing, and a worker that blocked two minutes after a beat closed stayed
invisible until the next one. The spool is the worker saying it instead.

It is also the one record of what went outward. Everything that reaches the
operator through `scripts/notify.sh` is spooled first, so anything reading the
spool can tell what has been *said* from what has only been *noticed* — and
never repeats back something the channel carried an hour before.

## Shape

One append-only JSONL file per instance, machine-local:

```
~/.factory/events/<instance>.jsonl
```

(Override the directory with `FACTORY_EVENTS_DIR`.) One line per utterance:

```json
{
  "ts": "2026-08-21T20:14:03Z",
  "instance": "acme",
  "from": "worker-acme-search-index",
  "kind": "blocked",
  "outward": false,
  "text": "turbopuffer preflight fails — need the prod key"
}
```

**`outward` is the field that matters.** `true` means it went outward through
`notify.sh` — a foreman's digest, on a build that has one — and the operator
has seen it; `false` means it was said on the floor and nobody outside has
heard it. Every other consumer of this file is downstream of that one
distinction.

`kind` is a closed list, so a reader can tell a blocker from a status line
without parsing prose:

| kind | who writes it | means |
|------|---------------|-------|
| `started` | worker | dispatched and working |
| `blocked` | worker | stopped, needs a decision — the decision is in `text` |
| `pr` | worker | opened a pull request — the URL is in `text` |
| `done` | worker | finished |
| `failed` | worker | ended without finishing, and not on a decision |
| `note` | worker | anything else worth the desk knowing; used sparingly |
| `posted` | `notify.sh` | went outward — a foreman's digest, or anything else a build sends through the seam |

## Writing

Workers, via the sixth standing instruction in their brief
([`factory-loop.md`](factory-loop.md), step 3):

```
scripts/factory-say.sh <instance> <session> <kind> "<one line>"
```

**Nothing this script writes goes outward.** The floor talks to the machine.
Eight workers narrating into a channel is the noise this arrangement exists to
avoid; a foreman, where the build has one, decides what a person needs to hear
and says it once, on its own clock ([`extending.md`](extending.md) §6).

`scripts/notify.sh` writes the other half — every outward post is spooled
first, and spooled whether or not the send succeeds or a channel is configured
at all. The record of what was said belongs to the machine and should not
depend on the network.

Neither can fail its caller. A lost line is not worth a failed beat or a dead
worker.

## Reading

```
scripts/factory-events.sh <instance>               new since your last read
scripts/factory-events.sh <instance> --peek        the same, without advancing
scripts/factory-events.sh <instance> --tail 20     back over old ground
scripts/factory-events.sh <instance> --count       how many unread (a number)
scripts/factory-events.sh <instance> --reader X    read as X, default reception
```

**Every reader keeps its own cursor** (`<instance>.cursor.<reader>`). The
gaffer, a foreman and reception read this file for different reasons on
different clocks; one shared position would mean whichever got there first
blinded the others. The gaffer passes `--reader gaffer` at step 6 and never
consumes anyone else's unread events.

## The readers

**The gaffer**, at step 6, reads what the floor said since its last beat before
it reads any panes. A `stuck` classification usually already has its
explanation sitting in the spool.

**The foreman**, on a build that has one, reads it as `--reader foreman` on
its own timer, alongside the beat records and the child ledger, and posts one
digest through `notify.sh` ([`extending.md`](extending.md) §6). A `blocked`
line a worker wrote two minutes after a beat closed reaches the operator on
the foreman's next pass rather than the gaffer's next beat.

**Reception**, opened from a workspace checkout when the operator asks, reads
it as `--reader reception` — over ssh, since the host runs no desk. What it
may do with what it reads lives in
[`reception-charter.md`](reception-charter.md); it makes no unprompted posts.

Nothing here wakes a process. The spool is written to be read, and the
readers come on their own clocks.

## Reported is not true

Every line here is an agent's testimony about itself. A worker saying it opened
a pull request is a claim; `gh` and the child ledger are facts, and *is
anything wedged?* is still `scripts/factory-health.sh` rather than something
inferred from the spool. Read it for the reason, not for the state, and quote
it as what somebody said.

## Say it when it changes

Five lines over a session is a talkative worker. The discipline is the same one
the WAITING ON YOU block runs on: a worker narrating every file it reads turns
the spool into something nobody reads, and then the blocker in the middle of it
goes unseen — which is the exact failure this was built to fix.
