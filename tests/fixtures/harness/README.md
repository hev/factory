# Sanitized recorded shapes

These fixtures project locally recorded September 2026 records onto fields used
by classification. Prompt text, account information, paths, session IDs and
unrelated telemetry are removed. No paid harness calls generated these records.

- codex-refusal: event_msg/token_count rate_limits.credits and task_complete
  error from historical rollout records. The task error was actually
  usage_limit_exceeded (not merely the plan's shorthand usage_limit). The reset
  text has no timezone and minute precision: 2026-09-19T02:12. The credit metadata
  is a projection from another historical token_count record of the same shape;
  this combined fixture is not claimed to be an unedited single-session trace.
- codex-exec-capacity: current controller events.jsonl error/turn.failed envelope.
  Capacity is other, not usage_limit. Observed successful exec streams contain
  turn.started and turn.completed with a usage object; tests construct a minimal
  projection of that envelope.
- claude-auth: historical assistant/isApiErrorMessage/authentication_failed
  record. No Claude print-mode result or usage-limit refusal was found in the
  local transcript scan. Unknown Claude shapes remain other. Scripted fake
  Classification results test failover orchestration, not vendor compatibility.

Tests prepend synthetic activity/malformed data for conservative replay checks.
