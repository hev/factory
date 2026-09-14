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

### The floor is not polled, and nothing wakes on a clock

There is no floor digest and no resync timer. Both existed to notice change by
watching files and the clock, and both charged a model turn for the watching:
92% of this controller's turns were woken by a `floor-change` or `resync`
carrying no information. A wake now comes only from a fact somebody asserted —
an approval, an assignment, a worker failing, a delivery, a message, steering.
`prepare_events` marks any surviving `floor-change` or `resync` reconciled
without a model turn, so a queue built under the old scheme drains rather than
needing to be cleared by hand.

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
write to tmux is never acknowledgment. Failed or abandoned model turns become
explicit ATTENTION records. A poll never retries an unchanged model input; recovery requires a durable steering
event naming the failed turn and its disposition. Timeouts terminate
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

## Deterministic assignment execution

FAC-28 applies to task-list assignments in event mode. Earlier exec transport
alone did not satisfy it: broad floor snapshots and periodic resync previously
invoked managers. Polls now observe facts without manufacturing model events.
Only commission, worker blocked/failed (including a failed launch or CI wait),
final task completion, and explicit steering invoke assignment judgment. Each
turn handles one durable event and records its key in its run receipt. Started,
PR, note, intermediate done, unchanged source reads and resync cost no model.

At commission the gaffer prepares linked worktrees and bounded briefs, then runs:

```
python3 scripts/factory-controller.py commission <gaffer-session> <tasks.json>
```

Only the owning gaffer's event turn may install the list. The input is an array
of tasks with unique `id`, `repo`, absolute `worktree`, absolute `brief`, `kind`
(`implementation` or `review`), and `after` (earlier task IDs). The ordered list
is the tie-breaker. Every implementation has a dependent independent review;
review briefs forbid mutation. Each worktree must be a linked worktree of its
named GitHub repository, inside configured scope. A canonical lane belongs to
one assignment until retirement. Ancestor/descendant lanes conflict too.
Commission persists `owner`, `repo_scope`, `worktree_lanes` and `tasks` in the
assignment before dispatch. Existing workers require explicit adoption by the
owner before commission; they are never guessed from a pane. Task identities
and dispatched task definitions cannot be replaced. A blocked-task decision may
append a new task/attempt with a new ID, preserving the old attempt and evidence.

The controller acts mechanically for that owner. Under a global dispatch lock
it reserves the task and child ledger before launching the configured worker
TUI, through the worker identity wrapper and shared cache lease, with its brief
on disk. It never submits text into an existing composer. The launch trampoline
claims a durable start receipt before starting the harness; replay cannot start
that attempt twice. A missing session after a start, or an ambiguous launch,
becomes a failed task requiring judgment, never a blind second worker. Restart
reconciles reservations and receipts before starting anything new.

Repository capacity is two workers and global capacity eight, including live
legacy sessions and unresolved reservations. A lane has at most one running
task; completion releases its execution slot, not assignment ownership. Hold,
source pause and winddown prohibit new dispatch; hold and source pause also
suppress judgment. Winddown permits completion and blocked/final judgment.
No task starts while the assignment has unhandled judgment or a failed turn.

Intermediate worker `done` advances the dependency list deterministically. A
registered CI wait prevents advancement while pending. A passed watch records
handoff to the commissioned independent review (or final acceptance), then is
acknowledged; failures generate a blocked decision. CI success and worker done
remain testimony, never acceptance. The final done invokes the gaffer to verify
all plan criteria, current PR head/checks, independent review and output gates,
record evidence and deliver through existing grants. If an operator-only gate
remains, record it and yield for explicit steering; never infer merge authority.
No timer retries acceptance. Notes and reports remain the continuity surface.

Health classifies every unconsumed event immediately: an active runner, or
ATTENTION naming hold, source pause, legacy transport, failed acknowledgment,
capacity/pending runner or recovery. Task wait reasons are visible too. A queued
event is never silently considered healthy because it is younger than 15m.
See `docs/deterministic-dispatch.md` for manual commissioning and recovery and
for the separate, gated installed-host acceptance procedure.

### Recording judgment

The owning acknowledged event turn uses these public commands (not direct edits
to task/queue runtime fields):

```
python3 scripts/factory-controller.py resolve-task <session> <task-id> "<evidence or replacement IDs>"
python3 scripts/factory-controller.py resolve-event <session> <original-event-key> "<evidence/disposition>"
python3 scripts/factory-controller.py delivery <session> <delivered|awaiting-gate|blocked> <evidence.md>
```

Resolution preserves original event payload, attempts and run ID and records
the current event as its cause. Resolve a blocked attempt only after inspecting
side effects; append new attempt IDs and change only undispatched dependencies
when retrying. Do not erase history or infer retry permission from renewed
capacity. Delivery records require a nonempty evidence file and a final/steering
turn, and retain the event key. `delivered` attests the gaffer's verified criteria,
independent review, current CI and permitted output-gate actions; the command
itself grants no merge authority. `awaiting-gate` names the remaining gate and
yields until steering. A successful model exit without commissioned tasks or a
current final-delivery record is incomplete output and remains ATTENTION.

A source status/timestamp change alone is observation. Changed description or
comment text is steering; where human actor IDs are configured, comments
attributed outside that set are excluded to avoid waking on bot report echoes.
Unattributed comments remain untrusted data, not approval. Source pause is
persisted separately even while the assignment runner holds its lock, and is
checked before commissioning, dispatch, resolution and delivery.
