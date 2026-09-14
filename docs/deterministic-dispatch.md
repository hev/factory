# Deterministic dispatch commissioning and recovery

Implementation status: the task dispatcher and isolated tests are present.
Controller and worker-cache integration are still required before enabling this
contract. The current controller still generates floor/resync model turns and
retries failed turns. Passing dispatcher tests is not end-to-end FAC-28 evidence.
The file boundary for those shared sources must be settled before integration.

## Commissioning interface

The owning gaffer commissions from its acknowledged event turn, after reading
the approved plan, doing preflight and preparing linked worktrees and bounded
briefs. The controller-facing interface to wire is:

```
python3 scripts/factory-controller.py commission <gaffer-session> <tasks.json>
```

Example `tasks.json` (replace paths with existing canonical linked worktrees and
absolute brief files):

```json
[
  {
    "id": "implement",
    "repo": "acme/app",
    "worktree": "/workspace/worktrees/app-change",
    "brief": "/state/briefs/app-change.md",
    "kind": "implementation",
    "after": []
  },
  {
    "id": "review",
    "repo": "acme/app",
    "worktree": "/workspace/worktrees/app-change",
    "brief": "/state/briefs/app-review.md",
    "kind": "review",
    "after": ["implement"]
  }
]
```

The review is a different worker and reads without mutating. Sequential tasks
may reuse their assignment's lane. Parallel tasks require different lanes.
Repository origin, current configured scope, dependency ordering, independent
review, canonical linked-worktree roots and cross-assignment ownership are
checked before accepting the list and again as appropriate before dispatch.
The normal worker harness/model/effort, role identity wrapper, shared build
cache lease, standing brief instructions, wire and child ledger remain in use.

## Local verification

Use Python 3.11 or newer (the existing sessions module imports `tomllib`):

```
python3 -m unittest discover -s tests -p test_dispatch.py -v
python3 -m unittest discover -s tests -p 'test_*.py' -v
```

These fixtures create local repositories and linked worktrees in temporary
directories. Terminal commands and model invocations are mocked, except for a
real local subprocess test of the boot receipt. They do not operate installed
controller state, sessions, identities, holds, approval receipts or Linear.
Existing controller tests still assert its pre-integration retry behavior;
those assertions must change with the controller integration.

## Recovery procedure after integration

1. Read the assignment, task states, `controller/workers/<worker>/launch.json`
   and `started.json`, original queue records and run receipts. Preserve all
   original event keys, payloads, attempts and run IDs. Do not reset a backlog
   of attempts to zero or requeue it wholesale after a credit/auth outage.
2. Let the deterministic observation reconciler retire only known `floor-change`
   and `resync` records, recording its disposition in the existing event. A
   `blocked` decision remains unhandled even when the assignment has retired.
   Health must identify it as an unhandled retired-assignment decision.
3. A reservation without a start receipt may recover the same named terminal;
   its launch identity must match. A durable start receipt prevents a second
   harness, even after the first terminal exits. Missing/ambiguous execution
   becomes an explicit failed decision. Never inject a brief into an occupied
   terminal to make recovery appear successful.
4. The owning gaffer handles recovery through a fresh steering event that names
   the original failed event. Inspect side effects before deciding whether work
   completed or needs a new task attempt. Preserve attempted task definitions;
   use a new task ID for another attempt and amend undispatched dependencies.
   Record the explicit disposition of old task/decision records. No timer
   infers this decision from elapsed time or renewed model capacity.
5. Keep source pause and holds effective. Winddown allows completion/judgment
   and forbids new tasks. Do not change another assignment's ownership or lane.

## Three scheduled polls and installed delivery evidence

This is a later, gated delivery tail, not a test performed by the source worker.
After the contract PR is operator-merged, code is deployed, affected managers
are restarted and the owning manager authorizes an isolated installed smoke:

1. Commission one small approved plan through the configured approval door.
   Save the approval event, assignment, task list, lane ownership, child ledgers
   and before-run model receipt inventory in its evidence directory.
2. Let the already configured scheduler perform three polls while no decision
   input changes. Preserve their `controller/polls.jsonl` lines and compare
   `controller/runs/<assignment>/*/prompt.txt` and turn receipts before/after.
   There must be zero new assignment model calls from these polls. Do not keep
   a model awake to sleep, tail logs or repeatedly inspect the scheduler.
3. Save wire events showing intermediate done → next worker without judgment,
   blocked/failed → one next-poll decision, and last done → one acceptance turn.
   Each actual manager call must name its durable source event key. Capture
   health for queued/running/blocked conditions, including immediate ATTENTION
   for unavailable capacity or failed acknowledgment.
4. Under the owning manager's explicit installed-test authorization, interrupt
   only the smoke controller wrapper at a reserved-launch boundary and restart
   it. Compare worker IDs, boot receipts and child ledgers: no duplicate worker.
   Do not run a second legacy manager alongside the controller.
5. Preserve independent review, exact-head CI readback, acceptance evidence and
   the output-gate disposition. Contract merges remain operator-only. Archive
   no plan as delivered while a required gate or verification remains open.

A successful source test run does not replace these installed observations.
The public runner is a Python process invoked by a scheduler an operator can
install by hand. No private overlay, webhook endpoint or paid provisioning is
needed; optional host deployment wiring is owned outside this public task.
