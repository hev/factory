> Approved source: https://linear.app/hevmind/issue/FAC-44/completed-legacy-workers-retain-all-slots-and-block-newly-commissioned

After deterministic-controller rollout, <issue id="dab83748-b65f-443a-a13e-e866a9d6d967" href="https://linear.app/hevmind/issue/LYR-106/kit-hev-up-health-probes-localhost-while-docker-publishes-127001-so-an">LYR-106</issue> and <issue id="6092e931-f0fa-4aad-8de5-9ee7a03c08fc" href="https://linear.app/hevmind/issue/LYR-107/kit-hev-init-silently-drops-every-config-block-it-doesnt-know-about">LYR-107</issue> passed approval, acknowledgment and commissioning but could not launch because all eight worker slots were occupied by retained legacy sessions. Four had completed handoffs: a merged implementation, two finished reviewers, and a docs worker with green CI. Three others were blocked by Claude usage-credit refusals. Manual attended harvest of the four finished workers allowed both new Kit implementations to launch on the next normal poll.

The legacy assignments have no executable commissioned task list. scripts/factory-dispatch.py calls the owner-scoped reaper only when record.tasks is nonempty. scripts/factory-reap.sh also relies on terminal output age, which interactive harness redraws can keep fresh after work finishes. Completed reviewer ledgers may have review evidence but no completed_at or PR field.

Done when:

* Deterministic reconciliation can harvest verified completed legacy workers without inventing commissioned tasks or converting descriptive checklists into executable work.
* A reviewer with durable completion evidence can release its session slot even without its own PR; shared worktrees and review evidence remain protected.
* Active CI handoffs, attached sessions, genuinely working workers and ambiguous/credit-blocked work remain protected and visible with a named owner recovery action.
* Completion is not inferred solely from PR existence or idle time. UI redraws cannot indefinitely hide an explicitly completed worker.
* A regression fixture fills all eight slots with a mixture of completed and unfinished legacy workers, queues new commissioned work, and proves only verified completed sessions are harvested and the new work launches without a manual restart or raised cap.
* The runtime contracts describe the legacy lifecycle and recovery path; repeated quiet polls do not require repeated manager judgments.

Observed 2026-09-22. Attended recovery preserved original ledgers, panes, worktrees and reports. Existing implementation: [https://github.com/hev/factory/pull/29](<https://github.com/hev/factory/pull/29>). This issue is follow-up work, not an approval to change runtime behavior.
