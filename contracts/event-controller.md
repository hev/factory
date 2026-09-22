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
Manager turns share `FACTORY_CONTROLLER_TURNS` slots (default **2**, a positive
integer), independently of worker capacity. Admission is serialized across
polls and direct `run` calls. Eligible idle sessions, including the foreman,
are ordered by the later of their oldest eligible event's creation time and
their last slot admission; session name breaks ties. Only the oldest waiter
may claim the next free slot. Admission resets its place even when it has an
older backlog, so a continuously eligible waiter cannot be overtaken repeatedly
by a busy neighbor. This guarantee assumes continued polls, finite contenders
and eventual slot release; it is not a wall-clock execution deadline.

An occupied slot or an older eligible waiter leaves the event pending without
changing attempts or retry timing. Each such deferral appends a timestamped
session/reason line to `controller/runner.log` and updates the assignment's
`controller_admission` counter, last-deferral timestamp and reason (the foreman
uses `controller/admission/foreman.json`). It is capacity waiting, not model
failure. Last admission is durable before execution; a crash before claiming
an event leaves it eligible, while an attempted/abandoned event still requires
explicit recovery. Held, source-paused, retired, legacy, nonlocal, active and
backoff-ineligible sessions do not reserve a place or block admission. Winddown
continues to allow judgment. Assignment and inherited slot fences remain in force.

Every poll writes each session's oldest pending-event age to `health.json`
(`pending`, including backoff, but excluding running/done/blocked events).
Age over **900 seconds** adds an ATTENTION problem naming the session, event,
age and threshold, including held or otherwise ineligible queues. This is an
alert bound at the next poll, not permission to retry or bypass a hold.

Inbox steering is identified by assignment, inbox path and the hash of its
message bytes. Direct delivery, observer discovery and the exact compatibility
message `Read durable foreman steering at <path>` share one event for identical
bytes, even after the file is archived under `inbox/done/`. Changed bytes or a
new inbox path create a new identity. Existing caller keys remain idempotent.
Old events without a captured content identity require attended evidence-based
reconciliation; missing files or similar prose alone never prove completion.

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

Only the owning gaffer's current acknowledged event turn may install the list.
Commission validates the running durable queue event, its run ID supplied by the
controller, and matching running turn/receipt before mutation. Missing, finished,
blocked or foreign event/turn contexts are refused. The assignment retains each
commission's event, run and task-list digest; identical replay is idempotent.
The input is an array of tasks with unique `id`, `repo`, absolute `worktree`, absolute `brief`, `kind`
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
it reserves the task and child ledger, including the expected launch identity,
before launching the configured worker TUI, through the worker identity wrapper and shared cache lease, with its brief
on disk. It never submits text into an existing composer. The launch trampoline
claims a durable start receipt before starting the harness; replay cannot start
that attempt twice. A missing session after a start, or an ambiguous launch,
becomes a failed task requiring judgment, never a blind second worker. Restart
reconciles reservations and receipts before starting anything new. A reservation
alone never authorizes harvest of a same-name terminal: the reaper verifies its
launch identity against the ledger, and preserves mismatched or unverified
terminals even after a rejected launch. Foreign ledgers are never overwritten.

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

### Legacy worker completion

An exec assignment need not have an executable task list for its recorded owner
to reclaim verified completed terminals. Each eligible poll runs the owner-scoped
reaper under the assignment and dispatch locks, including assignments with no
`tasks` or only a descriptive checklist. Checklists and lane ownership remain
unchanged; harvest does not commission, adopt, advance tasks or claim delivery.
Descriptive entries never participate in executable task capacity calculations.
Live legacy terminals still count against the unchanged eight global/two per
repository worker limits. Work queued earlier in a poll can launch on the next
normal poll after another owner releases capacity; no restart is necessary.

Harvest requires a ledger matching the session, instance, recorded parent and
current repository scope, with a timezone-qualified `dispatched_at`, plus either
an explicit `completed_at` at or after dispatch or a nonempty durable worker
`done` event at or after dispatch. A reviewer's `done` event can name its saved
review report; it needs neither its own PR nor a synthesized `completed_at`.
The event spool itself is retained completion testimony, not independent
acceptance. The harvest log copies the testimony, ledger and pane. Report files,
review evidence and shared worktrees remain in place. Worktree sweeping is
deferred for legacy assignments; terminal release does not release their lanes.

PR presence, idle age, a shell prompt and pane prose cannot establish completion.
A later `started`, `blocked` or `failed` event supersedes older completion
testimony. Invalid timestamps or ownership retain the worker for recovery.
A malformed complete spool record fails visibly unless it has the exact existing
attended quarantine disposition. Unfinished trailing spool writes are ignored.
Attached sessions, any unacknowledged CI watch (including ready results), known
busy or credit-refusal prompts, and mismatched/unverified recorded launch
identities veto harvest. Recent UI redraws alone do not veto explicit completion.
Unrecognized or uncertain completion remains protected; the reaper never asks a
model to interpret a pane. An absent session's incomplete ledger also remains,
while completed absent sessions retain their ledger and testimony in harvest.

`controller/reaper/<owner>.log` and assignment `worker_recovery` health records
name the owner, retained worker and concrete next action: handle/acknowledge CI,
wait for an attached reader, inspect launch evidence, inspect ongoing work, or
record an explicit completion/recovery disposition after checking side effects.
Credit failures grant no retry authority. Unchanged polls refresh diagnostics
without creating manager events or resetting attempts. Only existing explicit
steering/recovery routes can authorize further work. Hold/source pause suppress
harvest; the existing delivered-task cleanup exception and winddown semantics
remain. Approval, scope and operator-only output gates are unchanged.

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

### Attended legacy-data repair

The operator may run these commands on the configured home host after inspecting
the named data. Factory roles cannot invoke them:

```
python3 scripts/factory-controller.py quarantine-spool <instance> <line-number> "<evidence/disposition>"
python3 scripts/factory-controller.py archive-legacy-tasks <gaffer-session> "<evidence/disposition>"
```

Spool quarantine records the complete malformed line, its position, path and
prefix digest under `controller/recovery/spool/`. The dispatcher may skip only
that exact inspected malformed record. Changed prefixes or new corruption fail
loudly. Valid event objects and incomplete trailing writes cannot be quarantined.
The source log and all reader cursors remain intact; this is an explicit record
disposition, never an automatic parser fallback or a worker-completion claim.

Task archival requires the poll and assignment locks and rejects commissioned
lists, executable task identities and ambiguous lane ownership. It saves the full
assignment under `controller/recovery/assignments/`, retains the descriptive
checklist in `legacy_execution`, and converts owned legacy lane records into the
current ownership map. It empties only the uncommissioned checklist, preserving
approval, source pause, workers and lane reservations. It creates no commission,
decision resolution, approval or worker launch. The owning gaffer still decides
how existing approved work continues through normal acknowledged event commands.

A source status/timestamp change alone is observation. Changed description or
comment text is steering; where human actor IDs are configured, comments
attributed outside that set are excluded to avoid waking on bot report echoes.
Unattributed comments remain untrusted data, not approval. Source pause is
persisted separately even while the assignment runner holds its lock, and is
checked before commissioning, dispatch, resolution and delivery.
