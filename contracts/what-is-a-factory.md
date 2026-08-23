# What is a factory

The normative definition. Every other document here — the contracts, the
tooling, the extension points — should be explainable in its terms. When an
embodiment question comes up ("can we swap tmux for pods?", "can a factory run
in the cloud?"), this document is the acceptance test: it is a factory iff the
litmus below holds.

## The one-liner

**A factory is total autonomy inside mechanically-enforced boundaries, kept
alive by a heartbeat and made legible by ritual. The human is not in the
loop — the human is the boundary.**

## The three boundaries

All three belong to the operator. Inside them, autonomy is total — tokens are
leverage, never a throttle, and the factory never self-limits on spend.

1. **Intent (upstream).** Nothing self-willed. Work exists only as the
   decomposition of an approved plan in the workspace's `plans/active/`.
   The operator's **approval** is the only door intent enters through; an RFC
   nobody has moved is a proposal, never a mandate. The approval is a workflow
   state in Linear, and the commit into `plans/active/` is bookkeeping the
   gaffer does on their behalf ([`approvals.md`](approvals.md)) — writing the
   plan file in by hand does the same thing, and `plans/active/` on the plans
   branch remains the single sensor for what was decided.
2. **Scope (lateral).** A factory touches its workspace and the repos in
   its `repo_scope`, and nothing else — including account-global surfaces
   of a shared identity (notifications, invitations, identity-wide
   queries), which are partitioned by tooling or owned by exactly one
   parent. *The workspace defines the scope; tooling — not loop prose —
   enforces it.*
3. **Output (downstream).** Nothing leaves the building without a human
   act: merges, sends, payments, publishing, commitments. The factory's
   job is to make that act an approval of pre-verified work — never a
   first review.

## The six organs

- **Heartbeat.** The iteration is a pulse, not a daemon: each beat reads
  the world fresh (fetch + watermark), acts, reports, and schedules the
  next beat — five minutes later, busy or idle, because what the operator
  feels is how long an approval sits unnoticed, and a beat where nothing
  moved closes without doing much.
  Beats are idempotent — safe to re-run. **Silence is a defect**: a
  factory that doesn't beat isn't paused, it's down, and that must be
  visible from outside the factory ("WAITING ON YOU: nothing" is sent for
  exactly this reason).
- **Workers & peers.** Workers (children) are ephemeral and single-task:
  briefed — goal, constraints, identity, acceptance evidence — not
  trusted; namespaced to their factory (`<instance>-<task>` sessions);
  always harvested, never abandoned. Peers are sibling factories
  partitioned by scope. A factory may be dedicated to the *definition* and
  the shared tooling rather than to product work; it still never touches
  another factory's.
- **Gates.** The human boundary made procedural: the approved state, the three
  Linear triage labels (`queues.md`), draft-only rules, WAITING ON YOU. With one reviewer managing every factory, **the operator's
  attention is the fleet's scarcest resource** — every factory choice is
  judged by verified throughput per operator-minute, which is why the loop
  spends attention testing → blocked → dispatch → backlog, and blocks lead
  with direct URLs and read on a phone.
- **Tools.** Where a rule graduates from prose (trust) to mechanism
  (can't-do-otherwise). `scripts/factory-inbox.sh` is the archetype: the
  scope boundary stopped being a promise the day it shipped.
- **Rituals.** Fixed-shape ceremonies that make fleet state legible: the
  WAITING ON YOU block (always first, always live-verified, resolved items
  acknowledged once and never repeated), IN FLIGHT, the dated archive stamp,
  first-run-no-dispatch, steering-by-attachment.
  Rituals are the human-side interface; they are why one human can run N
  factories.
- **Ground.** Identity (one account, whatever `gh auth` already is), home
  (the `home_host` guard, launchd self-heal), and memory. Memory is three
  things, and only the third compounds: watermarks (where the loop got to),
  archives (never deleted, so provenance stays greppable), and **learnings**
  ([`learnings.md`](learnings.md)) — what the factory knows, written by the
  agent that paid for it and read by the next one before it starts. A beat
  reads *state* fresh; it does not re-derive *knowledge*, and a factory whose
  four-hundredth worker is as ignorant as its first is missing this organ
  rather than being admirably stateless.

## The litmus

A thing is a factory iff all six hold. `scripts/factory-health.sh` audits the
first of them; the rest are read off the tree.

1. **Alive** — it beats on its own, and a missed beat is visible outside
   the factory.
2. **Intent-gated** — every piece of work traces to an approved plan.
3. **Output-gated** — nothing irreversible or outward happens without a
   human act.
4. **Scope-clean** — it cannot (not "does not") touch a sibling's scope.
5. **Legible** — one glance at its ritual output = its current state.
6. **Recoverable** — it survives its host restarting, with memory intact.

## Standing invariants

Deliberate posture, not accident. Changing one takes a `[contract]` pull
request that names the invariant being changed.

- **Outbound is one-way.** Factories push outward; nothing commands a factory
  over the network.
- **Actuation only by attachment.** Steering happens by attaching to the
  session (tmux today). There is no inbound web control plane; any
  dashboard is read-only.
- **One reviewer.** The operator holds every gate. Fleet designs optimize the
  operator's attention, and nothing assumes a second reviewer exists.
- **The workspace defines the scope; tooling enforces it.**
