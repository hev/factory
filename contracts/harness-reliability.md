# Harness reliability

This contract specifies the reusable `scripts/factory_harness.py` component.
Dispatch integration is required before these rules affect the running factory.
It does not replace the controller, worker launcher, or role contracts.

Classify a finished, attempt-scoped stream as `ok`, `usage_limit`, `auth`, or
`other`. A Codex rollout's task_complete usage_limit/usage_limit_exceeded error
is a refusal; token_count credit metadata is diagnostic, never alone authority
to replay work. Current exec completion requires turn.started, turn.completed,
and exit zero without an error. Capacity failures are other. Malformed streams
classify as other; unknown activity prevents replay. Recorded Claude assistant
authentication_failed API errors classify as auth. Reset strings retain available
precision; a local minute without an offset requires an explicitly supplied
timezone for admission.
Unknown reset stays blocked. Claude formats without recorded evidence are not
invented: unsupported output is other, even on exit zero. A caller may supply a
Classification from a separately validated transport adapter; that adapter must
prove completion and refusal before execution, not infer them from process exit.

A usage-limit observation synchronously persists a host/harness breaker under
supplied `state/holds/harness/`, separate from manual instance holds and winddown.
Check admission immediately before each launch, not only at the start of a beat.
Existing admitted work is not killed. Expired breakers admit at or after reset;
old duplicate observations cannot reopen them. Concurrent observations serialize;
unknown reset dominates known resets, otherwise the latest reset wins. Corrupt or
unwritable state fails loudly. All state changes use locks, fsync and atomic rename.
The beat reader uses pending_reports then acknowledge_report after durable report
persistence. Stable report IDs allow deduplication after a crash; an unacknowledged
report may be delivered again, never silently lost. Repeated admission checks and
duplicate observations do not create reports. No automatic manual-hold clearing.

`harness_fallback` defaults to `claude`; empty string disables it. Supported names
are codex and claude. Optional `harness_models` maps each harness to its own model.
A model configured for the primary is never passed to another harness. Effort,
flags, MCP configuration and interactive/headless transport remain the launcher's
responsibility and must be validated per harness.

The attempt coordinator permits at most one primary and one fallback launch.
Fallback is allowed only for a usage_limit proven to precede substantive work,
or a primary suppressed before launch by its breaker. Auth, other, ambiguous
errors, or a limit after assistant/tool activity never authorize replay. Both
harnesses refusing produces blocked, never recursion. Each launch is durably
reserved before calling the adapter; an interrupted reservation stays blocked
for owner reconciliation and is never automatically resumed. Identical attempt
IDs and immutable brief/owner/task/event identity return the same receipt; reuse
with changed input fails. Receipts record both harness decisions, classifications,
models and brief digest, without copying prompts. This component neither launches
processes nor declares task acceptance: `ok` is transport completion only.

Callers retain role identity wrappers, ownership, home_host, scope, manual holds,
winddown, worktree/global caps, interactive worker attachment, CI protection,
acknowledgment, event provenance and output gates for each actual launch. A breaker
reset allows new work; it never resurrects a failed event or retries an unchanged
controller decision. An operator-authorized recovery needs a new attempt identity.
Hand-written identity executables printing tokens remain sufficient. No service,
paid provisioning, evaluator or model retry loop is introduced.
