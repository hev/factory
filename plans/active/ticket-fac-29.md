> Approved source: https://linear.app/hevmind/issue/FAC-29/harness-circuit-breaker-claude-failover-and-an-eval-budget

## What changes for the operator

Today a harness that has run out of credit is indistinguishable from a working
one. The factory keeps dispatching into it, every dispatch produces a session
record that did nothing, and the evaluator spends a grade on each of those
records. After this ships: a harness that answers with a usage-limit error stops
receiving dispatches, the brief re-runs on the other harness without an operator
step, and the evaluator never grades a session that has no assistant turn.

## Why now

From the incident host, 2026-09-13:

* **1,988 of 2,746 codex sessions hit** `usage_limit` (`credits.balance "0"`,
  reset Sep 19). The first was at 09:20 local; the factory kept dispatching for
  seven more hours.
* Each dead session is two turns — assignment prompt in, 402 out. Sample:
  `01a09cdf-9b1f`, `task_complete` with
  `error.codex_error_info: usage_limit`.
* 94 of them reached the evaluator as `role=worker, turns=2, outcome: 2`,
  summary *"no observable execution or terminal result"*. Each cost a `claude -p`.
* Not the first runaway: 2026-09-09 logged 2,135 eval rows against a 300–700 baseline.

Nothing in the loop notices. The refusal is machine-readable and we act on none of it.

## Is the evaluator worth keeping?

A fair question, and this RFC answers it with a measurement rather than an assumption:

* 5,458 graded rows to date; **57% marked** `poor`; **45 sessions ever marked**
  **reviewed** — 0.8% of what was graded was ever acted on.
* On the last two days cost attribution worked, evals were **$46.03 against**
  **$138.18** of session spend (09-07) and **$21.22 against $82.77** (09-08) —
  a quarter to a third of the floor's spend.
* Since 2026-09-09 both `eval_cost_usd` and `session_cost_usd` have logged `0`
  on every row. **The spend has been unmeasured for five days.** Step 4 restores
  it; step 5 puts the evaluator on trial with it.

## How success is measured

Replay 2026-09-13 against the new code: zero grades spent on refused sessions,
and dispatch to codex stops within one beat of the first `usage_limit` instead
of continuing for seven hours. Read it in `~/.factory/evals/evals.jsonl` and the
instance event log.

## Steps

1. **Classify the harness exit.** `codex exec` ends a refused run with
   `task_complete` carrying `error.codex_error_info: usage_limit` and a reset
   timestamp; `token_count` carries `credits.balance`. `claude -p` has its own
   shape. Add a classifier returning `ok | usage_limit | auth | other` plus the
   reset time. *Done when* a unit test feeds a recorded stillborn rollout in and
   gets `usage_limit` and `2026-09-19T02:12` out.
2. **Trip a breaker.** A `usage_limit` exit writes a hold for that harness
   (per host — the laptop and the mini have separate credit) under
   `~/.factory/holds` with the reset time. While the hold stands the parent
   dispatches nothing to that harness and reports it **once on the beat**, not
   once per attempt. *Done when* a simulated limit stops a dispatch loop inside
   one beat, and the next beat after the reset dispatches normally.
3. **Fail over to Claude.** Add `harness_fallback` to the factory toml, default
   `claude`. When the breaker trips, the same brief re-dispatches once on the
   fallback, with an event line naming both harnesses. *Done when* a codex
   worker refused on usage limit finishes its brief on claude with no operator
   step — and the fallback also refusing leaves a blocked ask, never a loop.
4. **Stop grading dead sessions; cap the sweep.** The evaluator skips any
   session with no assistant turn or a non-`ok` harness exit, recorded in
   `skipped.jsonl` with that reason (the skip path exists; the reason does not).
   Add a per-sweep grade cap and a daily eval budget in dollars, and refuse to
   start a sweep once the budget is spent. Fix the cost fields that have logged
   `0` since 09-09. *Done when* replaying 2026-09-13 grades none of the 1,988
   refused sessions, and a sweep past budget logs `budget spent` and exits 0.
5. **Put the evaluator on trial.** For two weeks, carry per row whether anyone
   ever acted on the grade (`reviewed.txt` already holds this) and report
   cost-per-acted-on-finding. *Done when* that number exists. If it is worse
   than a dollar per acted-on finding, the default flips to off and
   the evaluator scheduler unloads — **retiring the evaluator is a legitimate outcome of**
   **this RFC, not a failure of it.**

## Constraints and links

* Evaluator integration belongs to the separately maintained installation;
  public mechanisms must remain usable through hand-operated interfaces.
* Dispatch and harness detection in this repository — `pkg/factory/proc.go`,
  `contracts/factory-loop.md`, `contracts/autonomy.md`.
* **Out of scope:** the rubric itself, and buying more codex credit as the fix.
  Credit runs out again; a loop that doesn't notice is the defect.

## Steering recorded after approval

The source comments on 2026-09-14 retain steps 1–3 and the structural skip
floor. Keep scheduled evaluation disabled; re-enabling is not a delivery tail
and requires a fresh operator decision. Preserve ad-hoc evaluation. Classify
roles from trace structure and metadata, not only prompt prefixes. Replay
historical refused and evaluator sessions without spending model calls, and
retain gradeability of a short worker trace that calls a tool or reports a
terminal result. Audit approximately twenty historical sessions to distinguish
misclassified managers/evaluators from actual worker dispatch defects,
reporting sample limits. The original sweep budget and two-week trial above
remain recorded for provenance; reconcile their disposition with the shutdown
steering before claiming completion.
