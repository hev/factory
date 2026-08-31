# The event spool

What the floor says, in words, while it is still true.

Before this existed a worker had no voice. Its state could only be inferred —
a pane snapshot, a child-ledger entry, a harvest log after the fact — so *why
has that one been quiet for an hour* was answered by reading pixels and
guessing, and a worker that blocked two minutes after a beat closed stayed
invisible until the next one. The spool is the worker saying it instead.

It also closes the other half of that gap, which nobody had noticed was open:
the front desk had no idea what the operator had already been told. The gaffer
posted the WAITING ON YOU block to Slack and reception never saw it, so it
could repeat back something the channel had carried an hour before.

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

**`outward` is the field that matters.** `true` means it went to Slack and the
operator has seen it; `false` means it was said to the desk and nobody outside
has heard it. Every other consumer of this file is downstream of that one
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
| `posted` | `notify.sh` | went outward — the block, a dispatch line, a desk post |

## Writing

Workers, via the fourth standing instruction in their brief
([`factory-loop.md`](factory-loop.md), step 3):

```
scripts/factory-say.sh <instance> <session> <kind> "<one line>"
```

**Nothing this script writes reaches Slack.** The channel is one job's report,
and eight workers narrating into it is the noise a per-factory channel exists
to avoid. The audience is the front desk, which decides what a person needs to
hear.

`scripts/notify.sh` writes the other half — every outward post is spooled
first, and spooled whether or not the send succeeds or Slack is configured at
all. The record of what the factory said belongs to the desk and should not
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

**Every reader keeps its own cursor** (`<instance>.cursor.<reader>`). The desk
and the gaffer read this file for different reasons on different clocks; one
shared position would mean whichever got there first blinded the other. The
gaffer passes `--reader gaffer` at step 6 and never consumes the desk's unread
events.

## The two readers

**The gaffer**, at step 6, reads what the floor said since its last beat before
it reads any panes. A `stuck` classification usually already has its
explanation sitting in the spool.

**The floor watcher** reads it on the machine's timer after the gaffers. It
posts each new, non-outward `blocked` line through `notify.sh` and advances the
cursor in `~/.factory/reception/<instance>/spoken`. Other worker lines remain
available to reception when the operator opens it, but wake no process.

What reception may do with what it reads lives in
[`reception-charter.md`](reception-charter.md); it makes no unprompted posts.

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
