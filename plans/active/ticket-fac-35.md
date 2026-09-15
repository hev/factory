> Approved source: https://linear.app/hevmind/issue/FAC-35/run-turn-drops-a-turn-silently-when-no-global-slot-is-free-so-a

## Problem

`scripts/factory-controller.py` `run_turn()` acquires one of `FACTORY_CONTROLLER_TURNS` global slot locks (default **2**) after it has already collected the pending events. When no slot is free it does this:

```python
if slot is None:
    return
```

Three things go wrong at once:

* **The drop is silent.** No log line, no event write, no `attempts` increment, nothing in `runner.log`. From the outside a starved assignment is indistinguishable from one with nothing to do.
* **There is no fairness and no aging.** Every poll calls `spawn()` for every eligible record and the slots go to whoever reaches them first. An assignment whose events arrive on every poll takes a slot every time; one behind it never does. This is deterministic starvation, not contention that resolves.
* `attempts` **stays at 0**, so the retry/backoff and the three-strike blocked path never engage. A starved event is not a failed event, so nothing escalates it.

## Evidence

`gaffer-factory-ticket-fac-29` (<issue id="0a6af4d7-dcc3-4887-8638-2956369ec3ae" href="https://linear.app/hevmind/issue/FAC-29/harness-circuit-breaker-claude-failover-and-an-eval-budget">FAC-29</issue>, approved into Todo at 01:46:28Z) was assigned at 2026-09-14T02:10:16Z. At 18:39Z — **16.5 hours later** — `~/.factory/controller/runs/gaffer-factory-ticket-fac-29/` was empty, there was no turn file, and its queue held **226 events, all pending, all** `attempts=0`: the `approved` event itself, 8 `linear-update`, 2 `steering`, and the rest floor churn.

Traced against the live record with `sys.settrace` filtered to `run_turn` (read-only):

```
L326: with gate(BASE / 'locks' / (s.name(session) + '.lock'), False) as own:
L331: if role == 'gaffer' and (not record or record['status'] == 'retired' or ...
L335: if not s.at_home(cfg):
L338: if role == 'gaffer' and record.get('transport') != 'exec':
L345: if not pending:
L349: for n in range(int(os.environ.get('FACTORY_CONTROLLER_TURNS', '2'))):
L352:     if slot_file:            # slot 0 — not acquired
L352:     if slot_file:            # slot 1 — not acquired
L356: if slot is None:
L357:     return
returned
```

Every gate passed. It died on the slot, and said nothing.

Holding the whole `lyr` instance (four gaffers) was **not** sufficient to free it: `gaffer-factory-ticket-fac-27` and `gaffer-factory-ticket-fac-28` alone keep both slots, because each has pending events on every poll. Starting the turn required `FACTORY_CONTROLLER_TURNS=3` on a hand-run `factory-controller.py run` — it then started immediately (run `1789411229734891000`, 18:40:29Z), which confirms the slot was the only thing in the way.

`scripts/factory-health.sh` does report `event queued without a runner for over 15m`, which is the one signal that exists — but it names a symptom 15 minutes late and the controller's own `health.json` recorded `problems: []` throughout.

Related: <issue id="8ae9c526-f3d2-4383-9c8b-a2236ee610cf" href="https://linear.app/hevmind/issue/FAC-33/the-floor-digest-wakes-gaffers-on-file-mtime-so-a-waiting-ci-record">FAC-33</issue> is why <issue id="43397ea6-7f08-434f-bf47-a327d14422aa" href="https://linear.app/hevmind/issue/FAC-27/loop-schedules-are-configured-in-a-browser-create-edit-pause-and-run">FAC-27</issue> and <issue id="b577dcd7-bf12-4c03-abf5-eb0319943195" href="https://linear.app/hevmind/issue/FAC-28/an-approved-plan-runs-for-the-cost-of-the-work-not-the-cost-of">FAC-28</issue> have events on every poll in the first place. Fixing <issue id="8ae9c526-f3d2-4383-9c8b-a2236ee610cf" href="https://linear.app/hevmind/issue/FAC-33/the-floor-digest-wakes-gaffers-on-file-mtime-so-a-waiting-ci-record">FAC-33</issue> makes this bug much harder to hit; it does not fix it.

## Done when

* A turn that cannot get a slot is recorded rather than dropped: a line in `runner.log` naming the session, and a counter or timestamp on the assignment that a reader can see. Demonstrated by filling every slot and observing the record for a starved session.
* Slot handout is fair: an assignment that has waited longer takes precedence over one that just ran, so no assignment can be starved indefinitely by a busier neighbour. Demonstrated with more eligible assignments than slots over several polls, showing every one gets a turn.
* A session whose oldest pending event exceeds a bounded age becomes a visible problem in the controller's own `health.json`, not only in `factory-health.sh`.
* `contracts/event-controller.md` states the concurrency limit, what happens when it is reached, and the fairness guarantee — today the contract says events are delivered at-least-once with retry and backoff, which a silently dropped turn does not honour.

Filed by reception at the operator's request, 2026-09-14.
