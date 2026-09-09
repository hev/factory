# CI completion handoff

Waiting for CI is machine work. No worker or gaffer spends inference repeatedly
sleeping, querying GitHub, tailing a watch log, or asking whether an unchanged
run is finished. The same rule applies to builds, deploys and any other long
external wait: use a deterministic watcher/completion signal, or yield the task
for the next controller observation. A longer model-driven backoff is still
polling inference.

## Worker

After pushing and opening the PR, perform the source checks and self-review
that can finish now. Register the exact PR head using the factory checkout's
front door (the gaffer fills in the command in the brief):

```
/path/to/factory/factory ci wait <instance> <worker-session> <owner/repo> <pr>
```

This requires the worker's existing child ledger. It copies that ledger into
`~/.factory/ci/<instance>/<id>.json`, including the plan identity and a copy of the brief text when a brief is named,
and records the PR head, registration time and a 24-hour deadline. Repository
scope and `home_host` are enforced before reading GitHub. Ordinary `gh` auth or
the existing role identity seam suffices; no service, public endpoint or new
credential is required.

A successful registration means **waiting for CI**, not done or blocked on a
person. Report one `note` on the worker wire with the watch ID and PR URL, say
what remains, and end the turn. Do not exit/restart the harness just to wait,
self-schedule a reminder, tail a log, or keep checking the watch. The gaffer
owns resumption. A failed registration is not a handoff: report that failure
and leave the task recoverable; do not replace it with a model polling loop.

Repeated registration of the same worker/PR/head returns the same ID. Another
worker or head cannot overwrite an unhandled watch. The reaper preserves a
registered worker's pane, ledger and worktree until the event is handled, even
if its pane is idle or the session has disappeared. A paused worker must not
be marked complete, nudged as stuck, or dispatched a second time while waiting.

## Controller and completion hook

```
factory ci poll <instance>
factory ci list <instance>
factory ci list <instance> --ready
```

Both runtime wrappers run `poll` on the existing home-host timer before tending
the floor. The built-in transport polls GitHub **without a model**; it is not a
GitHub webhook server. An existing trusted completion hook may also invoke
`factory ci poll <instance>`. The command always re-reads GitHub; a supplied
webhook payload never declares success. The one-shot sensor wakes the gaffer
only for a ready event, on the next eligible timer tick. A resident gaffer
reads ready events on its next beat. No worker receives unsolicited tmux keys
from the watcher, and no listener or scheduler is provisioned by registration.

A watch remains `waiting` while checks are pending or no checks are reported.
It becomes ready once, with one of these states:

- `passed`: every reported check passes or is skipped. This is a verification
  trigger, not acceptance, merge approval, or proof that required checks exist.
- `failed`: a reported check fails or is cancelled, even if other checks run.
- `superseded`: the PR head changed. Old results cannot certify the new head.
- `closed`: the PR closed or merged elsewhere.
- `timed-out`: 24 hours elapsed without a terminal result, including no checks.
- `unavailable`: three consecutive GitHub reads failed, or the repo left scope.

The PR head is checked before and after reading checks. Transient read failures
remain durable diagnostics, not recurring model wakeups. A successful read
resets the failure streak. A ready watch is not polled again. Atomic writes and
process/file locks protect concurrent timer/hook invocations and restarts.
Unwritable or corrupt state fails loudly. Authentication/transport failures and
an empty check list never become green.

## Gaffer

Read ready events at step 6 and inspect the saved ledger/brief. Resume the
original idle worker to verify success or repair failure. If its session has
gone, reconstruct the same task from the saved ledger under the normal dispatch
rules and worker limits. Re-read current PR head and checks before acceptance;
CI readiness is never approval. A superseded event needs fresh verification
and, if still waiting, a new watch for the current head. Closed PRs need a
recorded disposition. Missing CI, unavailable auth and timeout need an explicit
route rather than a blind re-registration loop.

Only after recording the disposition or successfully resuming/re-dispatching:

```
factory ci ack <instance> <id>
```

Acknowledgement removes that exact ready event and releases reaper protection.
It cannot acknowledge a pending watch. A crashed beat leaves the event ready
for the next beat. Before retrying a completion after a crash, inspect the named
worker and its wire/ledger state; do not send duplicate instructions to a
worker already acting on it. Never acknowledge first and hope dispatch succeeds.

A gaffer handling other work may read `list` once, but must not repeatedly
query pending runs itself. Pending PRs with a registered watch are excluded
from ordinary open-PR pickup. Completion resumes the same approved goal; it
creates no issue, RFC, permission grant, or additional approval door.
