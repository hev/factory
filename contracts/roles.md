# Roles and sessions

Normative for the `sessions` runtime. This replaces the instance-parent shape:
reception is the operator's hands, the foreman runs the factory, gaffers manage
assignments, and workers implement them. Legacy `resident` and `one-shot`
configurations remain explicit compatibility runtimes until migrated.

| Role | Lifetime and home | Authority and normal interlocutor |
|---|---|---|
| Reception | Attended skill on the operator's laptop | Acts on the operator's explicit decisions, as the operator; talks to the foreman |
| Foreman | Persistent host session, `foreman` | Operates configured factories on this home host as the factory identity; talks directly to the operator; commissions and supervises gaffers |
| Gaffer | On demand, persistent for an approved plan, `gaffer-<instance>-<slug>` | Decomposes its assignment, owns worktrees and workers, verifies delivery; answers only to the foreman |
| Worker | Task-lived, `worker-<instance>-<slug>` | Implements and verifies a bounded task; directed only by its owning gaffer |

All host sessions use the default tmux server. tmux is the shared floor and
an observation/intervention surface, not an alternative chain of command.
Humans can attach anywhere, but normal operation never depends on talking to
a gaffer or worker. Record an exceptional intervention in the assignment's
notes and relay it to its manager before resuming on changed assumptions.

Identity and authority are separate. Direct conversation with the operator
does not give the foreman the operator's credentials or approval powers.
`identity/<role>` remains an executable printing a token on stdout; it requires
no provisioning service. Without hooks, ambient identity remains supported,
but it does not establish account separation or expand approval authority.

Only the two doors in `approvals.md` admit work: the operator's Linear state
transition, or their merge of a plan PR. The foreman verifies approval and
materializes Linear plans; in PR mode it never merges the approval PR or
creates approved intent itself. Neither gaffers nor workers approve intent.
Output gates and explicit self-merge grants remain in force. A direct chat
with the foreman can steer approved work; it cannot substitute for approval.

The foreman owns intake, factory-wide queues, assignment registry, reconciliation,
and operator-facing reports. Each gaffer owns only its assigned plan, worker
ledger entries, worktrees, reviews, and evidence. Workers do not independently
recruit other workers or accept assignments from reception or the foreman.
Independent implementation and review run in separate worker sessions.

Durable records, not an immortal context window, make sessions long running.
Restarted managers reconcile existing assignments and workers before dispatch.
A plan has at most one active gaffer; the foreman coordinates repository lanes
across plans and respects each instance's team and repository scope. Never run
legacy parent loops alongside a sessions foreman for the same instance.
