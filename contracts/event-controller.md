# Event controller — unattended execution without terminal input

When `~/.factory/controller/enabled` exists, this contract overrides the
sessions timer/intake and manager-lifetime clauses of the older charters.
The host/operator enables the mode after migration. Model workers do not
change controller configuration, approval receipts or repo scope.

## Roles

The deterministic controller polls the configured Linear team for approved
RFC/bug/chore/task issues, exhausts pagination, validates attribution and
configured repo scope, and persists an assignment before launching its gaffer.
It also observes worker ledgers, CI records, inboxes and floor events. Polling
is the initial event source; no public webhook endpoint is required. A slow
resync covers missed changes. A quiet event key is deduplicated on disk.

### What counts as a floor change

A wake costs a model turn, so the floor digest carries only facts the
assignment does not already own, and only its own:

- Worker ledgers, CI records and the assignment inbox, scoped to **this**
  instance and session. A digest that reaches wider makes one instance's
  workers wake another's assignments, which is a cost with no signal in it.
- Live worker sessions named `worker-<instance>-*`, so a worker vanishing
  without updating its ledger is still a change. The read is scoped by that
  prefix: a machine-wide session list makes every worker on the box, the
  foreman, and a human's stray shell move every assignment's digest.
- **Never the instance's own event spool.** A gaffer's turn appends to
  `events/<instance>.jsonl`, so digesting it lets a turn's own output wake the
  turn that wrote it.

### The resync backstop

A resync carries no information. It exists only to catch a source change that
intake missed, and every one it fires costs a full model turn against an
unchanged floor. Its key is a wall-clock bucket, so the bucket width is the
wake rate: **six hours, four wakes per assignment per day**, set by
`RESYNC_INTERVAL` in `scripts/factory-controller.py`. Widen it freely — the
only thing the interval buys is how long a missed source change may sit
unnoticed. Narrowing it is a decision about that latency, never a default,
and the code and this clause change together or not at all.

The foreman watches the floor, reports stalls/conflicts and takes steering
from the operator or reception. The interactive foreman is not required for
routine intake or progress. It must not run a second approval intake loop,
start duplicate assignments or inject wakes into tmux. Changed gaffer reports trigger an observer turn; explicit steering
uses the durable message command; unattended steering turns are serialized
by the controller, while the interactive observer consults their receipts.

Gaffers retain durable assignment identity and worker ownership. They run
bounded `codex exec --json` turns when events arrive, reconstructing context
from notes, reports and the approved plan. One model turn is allowed per
assignment. Complete the current reconciliation, persist notes and return;
do not wait in an interactive input box or establish a recurring model loop.
Workers retain their existing task/verification contracts and may stay in
tmux. Only their owning gaffer directs them. Repository/global worker limits,
separate implementation/review, existing output gates and holds remain.

## Delivery and recovery

The controller stores pending/running/done/blocked events before execution.
A kernel lock fences each assignment; global lock slots bound concurrent
manager turns. Start is acknowledged only by `turn.started`, completion only
by successful exit plus `turn.completed`. Process existence or a successful
write to tmux is never acknowledgment. Failed events retry with backoff,
then become visible blocked records after three attempts. Timeouts terminate
only that runner's model process group, preserving workers and worktrees.
Locks are inherited by the model process so a killed wrapper cannot cause a
second owner while the first model still runs.
When every global slot is busy the runner waits for one (`FACTORY_SLOT_WAIT`
seconds, default 1500, polling every `FACTORY_SLOT_POLL` seconds, default 10)
rather than dropping the turn; while it waits it holds its assignment lock, so
no second runner is spawned for that assignment. The wait is logged in
`runner.log` and stamped on the assignment record as `slot_wait`; a runner
that gives up leaves the stamp, and `health` reports an assignment starved for
over fifteen minutes. Slots are handed to whichever waiter tries next, so no
assignment can be starved indefinitely by a busier neighbour; strict
longest-waiter precedence is not guaranteed.

Event processing is at-least-once. A crash after an external operation may
occur before the receipt is saved; reconcile source state and existing worker
ownership before repeating any operation. Never assume exactly-once writes.
Periodic controller polls recover abandoned claims; the host scheduler is
independent of the foreman. Health measures poll freshness, failed delivery
and abandoned execution, not an idle observer's last conversational turn.

## Approval and scope

Approved state and an eligible label remain required. Attribution comes from
an allowed actor in source history; creation directly in the approved state
can use its creator when timestamps match. Never assume the creator made a
later state change. The instance may configure `linear_approval_actors` as
human IDs. Missing attribution produces an explicit intake problem.

An attended operator/reception action may record an approval receipt under
`controller/approvals/<issue>.json`: actor, team, source URL, description
SHA256, timestamp and required repo scope. This records the operator's actual
approved-state action when the provider omits history actors; it is not an
agent-inferred approval or permission to move Todo. Factory identities must
not author receipts. The issue must still be in the approved state at intake,
and body/team must match. After performing an explicitly authorized state
move, attended reception records the readback on the home host with:

```sh
python3 scripts/factory-controller.py receipt acme ENG-12 <human-id> --repo owner/repo
```

The command uses the configured factory read identity, requires the source
still be approved and in the correct team, checks the configured human ID,
and refuses foreman/gaffer/worker callers. It does not move Linear state or
authorize a new scope. Never invoke it merely because an agent requested it.

 A gaffer accepts validated controller attribution
rather than re-gating on the same absent provider field.

Linear plan materialization is durable local bookkeeping with the source URL
and approval receipt in the assignment record. The gaffer commits/publishes
that bookkeeping under the existing repo gates; an unmerged bookkeeping PR
is not a second approval door. Existing PR-door instances remain explicitly
reported as needing legacy intake/adoption until implemented here; do not
silently adopt unapproved local plan files.

## Migration and operations

Deploy code before enabling. Archive legacy manager pane output and replace
only the old gaffer process with an exec-transport assignment; keep the same
assignment and worker parent IDs, notes and worktrees. Restart the foreman
as an observer with this contract. Never lift a held factory during migration.
Receipt/run logs live under `controller/`; secrets are not copied into them.
The operator can disable event mode for rollback only after stopping active
controller turns; do not run legacy and event managers concurrently.

Validation must cover duplicate events, crash recovery, occupied terminal
input, process failure without completion, approval/scope rejection, held
instances, concurrency, and three scheduled unattended polls on the host.
