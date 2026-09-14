> Approved source: https://linear.app/hevmind/issue/FAC-28/an-approved-plan-runs-for-the-cost-of-the-work-not-the-cost-of

As the operator, I want an approved plan executed without a manager session waking on a timer to look at it, so that my inference budget buys implementation instead of polling.

## Why now

The gaffer bundles three jobs with different cost profiles:

* **Decomposition** — plan into bounded tasks. Needs a model. Happens once, at commission.
* **Dispatch and tending** — start a worker, watch a pane, honour capacity, assign a worktree lane, reap the dead. Needs no model at all. This is the beat that wakes every interval to conclude nothing changed, and it contradicts the loop's own principle that a deterministic sensor gates every model invocation.
* **Judgment** — a worker came back `blocked` or `failed`; is the acceptance evidence real; is the plan delivered. Needs a model, but only on an event.

Recorded burn on this fleet was the gaffers, not the workers. Right now on the mini there are seven live worker tmux sessions and zero gaffers: the manager layer is the layer that is not there, and `lyr` health reports `event queued without a runner for over 15m`. The events already are the wake mechanism; the persistent session is the part missing to consume them.

## Acceptance criteria

* A plan runs from commission to delivery with zero timer-driven model invocations. Every model call in its ledger is traceable to the commission itself or to one spool event.
* `gaffers/<slug>.json` still exists for every in-flight plan, still names exactly one owner, its repo scope and its worktree lanes. Two plans never share a lane.
* A worker emitting `blocked` or `failed` gets a model decision within one controller poll, with no session held open beforehand.
* A plan whose last task emits `done` is verified against the plan's acceptance criteria and delivered without an operator touching it.
* `factory-health.sh` never reports a queued event without a runner. A queued event either has a runner or is an explicit ATTENTION naming why.
* The public build still satisfies the seam rule: the runner is described in `contracts/` and an operator can stand it up by hand, with no provisioning to buy.

## How to test it

1. On the mini, approve one small RFC by moving it to Todo from a phone.
2. Watch `~/.factory/events/factory.jsonl` — one `started` per task, `done` on each, `pr` where one opens.
3. Count model invocations for that plan in the child ledger and compare to the event count. Timer-driven calls should be zero; on 2026-09-08 this instance ran 130 beats in a day.
4. `./scripts/factory-health.sh factory` reports no runnerless event for the plan's lifetime.
5. Kill the controller mid-plan and restart it. The plan resumes from `gaffers/<slug>.json` and the spool, with no duplicate worker.

## Design

Keep the gaffer as a **record**. Kill it as a **session**.

* The assignment record is the ownership wall — one plan, one owner, non-overlapping worktree lanes, repo scope. It costs nothing and it is what stops two plans stepping on each other. It stays.
* The event controller does dispatch. `done` on task N fires task N+1 from the task list written at commission. Deterministic, no tokens.
* Model invocations happen at exactly three moments: **commission** (decompose the plan into the task list, write it to the assignment record), `blocked` **/** `failed` (unblock, retry, re-scope, or escalate to the foreman), **last task** `done` (verify acceptance, deliver).
* The foreman stays persistent. Its persistence is justified — it is the operator's interlocutor, and there is one, not one per plan.
* Accumulated context moves to `gaffers/<slug>.notes.md`, which each invocation reads. That file already exists in the contract.

## What this costs

Mid-flight coaching. A live gaffer can steer a running worker before it finishes going wrong; an event-driven one reacts at `blocked` or `done`. A worker quietly producing garbage for forty minutes is caught later than it is today. Accept this unless the ledger shows gaffers actually catching that case.

## Constraints

* Event-controller mode and the runnerless-event health string are not in the public checkout — they are overlay-side. The plan must say where the runner lives and how the split survives the one-way dependency in AGENTS.md.
* Contracts to change: `roles.md`, `gaffer-charter.md`, the dispatch half of `factory-loop.md`, and a runner section in `events.md`. Behaviour changes by changing the contract first.
* Legacy `resident` and `one-shot` runtimes are out of scope.

Filed by reception from a design conversation with the operator, 2026-09-13.
