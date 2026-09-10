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

The foreman watches the floor, reports stalls/conflicts and takes steering
from the operator or reception. The interactive foreman is not required for
routine intake or progress. It must not run a second approval intake loop,
start duplicate assignments or inject wakes into tmux. Explicit steering
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
and body/team must match. A gaffer accepts validated controller attribution
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
