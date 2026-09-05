# A session's transcript can be tied to the work it did

Draft. Not in `plans/active/`, so nothing dispatches against it.

## What changes

Somebody asks "what did the workers on the scoped-key-leak plan actually do,
and which one opened the PR?" and gets an answer. Today that question dies at
the first step: the factory knows a worker as `worker-lyr-scoped-key-leak` with
a plan, an RFC and a PR number, while its harness writes the transcript to
`~/.claude/projects/-Users-hev-workspace-lyr/f18748dc-….jsonl`. Nothing
connects the two. On this host that is 382 transcripts and 110 MB of history
that cannot be attributed to a single plan, issue or pull request.

The fix is one field written at dispatch. Everything downstream — an archive,
a search index, a cost report, an eval set — is unblocked by it, and none of it
is possible without it.

## Why now

An external trace archive is being built against this floor
(`hev/kit` RFC 0003). It can normalize and index every transcript on the
machine, but the columns that make a *factory's* archive worth more than a pile
of chat logs — instance, plan, RFC, issue, PR — are ones only the factory can
supply. Capturing 110 MB unattributed and backfilling the join later means
re-indexing the archive.

It is also the cheapest moment. The dispatch path already writes a child-ledger
entry with every one of those fields; the harness session id is the one thing it
does not record.

## How success is measured

Given a plan slug, every transcript produced under it can be listed, and every
transcript can name the plan, issue and PR it belongs to. Concretely: a join
from `~/.factory/children/<session>.json` to a transcript path, with no
guessing from timestamps or working directories.

## Shape

**Record the harness session id in the child ledger.** `contracts/child-ledger.md`
gains one optional field:

```json
"harness_session": "f18748dc-c235-4020-941b-6aebd1461c00",
"harness": "claude_code"
```

Optional because a worker whose harness does not expose a session id is still a
valid worker, and the ledger has never required a field the floor cannot always
supply.

The awkward part, and the part to design rather than assume: the id is minted by
the harness *after* the session starts, so dispatch cannot write it up front. The
options are for the RFC to settle —

- the worker writes it itself, as a fifth standing instruction in its brief
  (consistent with how it already reports `started` / `blocked` / `pr` to the
  event spool, and the only option that needs no new machinery);
- the reaper resolves it at harvest, matching newest transcript under the
  worker's `cwd` against `dispatched_at`;
- a harness flag pins the id at launch, where one exists.

The first is the most in keeping with the contracts: a worker has a voice, and
this is one more thing it says about itself.

**Stamp the event spool.** `~/.factory/events/<instance>.jsonl` lines already
carry `instance` and `from`. Adding `plan` and `harness_session` makes the
semantic events (`blocked`, `pr`, `done`, `failed`) joinable to the transcript
that produced them — which is what turns "this worker was blocked for an hour"
into "here is the hour."

## What this is not

- **Not a tracing system.** This plan makes traces joinable; it stores nothing,
  indexes nothing and ships nothing off the machine. Anything that does lives
  outside this repo.
- **Not a new dependency.** The public build must keep working with no archive
  attached. These fields are written whether or not anything reads them, the
  same way the child ledger already outlives the picker that reads it.
- **Not estate.** No hostnames, no endpoints, no credentials — a session id and
  a plan slug, both already the factory's own vocabulary.

## Open questions

- Which of the three capture points above, and does the answer differ for
  `codex` workers (which mint a rollout path, not a uuid)?
- Reception and the gaffer produce transcripts too. A gaffer beat is a process,
  not a session — is a beat's transcript worth joining to the beat record in
  `~/.factory/beats`, or is worker attribution the whole ask?
- Does the ledger entry survive the worker? Child-ledger files are for *live*
  children; the join is most valuable after the fact, which may mean the harvest
  log is the right home for the final record.
