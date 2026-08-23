# The three queues

Work in the operator's court is made legible by three Linear markers. Every
factory prioritizes these because *the operator's attention is the fleet's
scarcest resource* (see the Gates organ in
[`what-is-a-factory.md`](what-is-a-factory.md)).

**Linear is a human surface.** Everything in this file exists because a person
has to do something. Machine work has no presence here at all: the factory's
work list is the plan document in `plans/active/`, and what is in flight
against it is the child ledger ([`child-ledger.md`](child-ledger.md)). The
factory never files an issue to remember something or to hand itself a task —
an issue is an ask, and an ask with no human on the other end is noise on a
tracker somebody else reads.

That rule is why there are no sub-issues per plan step. A tracker that grows a
row for every dispatched worker is the machine filing tickets at a person.

## The three

| Queue | Marker | Meaning | Applied when | Cleared when |
|-------|--------|---------|--------------|--------------|
| **Ready for Testing** | `linear_review_state`, else a `testing` label | Factory-verified work that **genuinely needs the operator pre-merge**: contract-class changes, ASK resolutions, hard lines (secrets, payments, outbound). The issue carries the pull request URL; the review happens on GitHub. | The gaffer verifies the PR and posts "factory verified: …" on work outside the self-merge grants. | The operator merges (or requests changes → back to work). |
| **Blocked** | a `blocked` label, always | Can't proceed without a decision or a `[human step]`. Backs the WAITING ON YOU block. | A worker ends on a blocker only the operator can clear, or triage finds a decision only they can make. | The decision lands / the step is done. |
| **Backlog** | `linear_backlog_state`, else a `backlog` label | An idea captured but *not* in `plans/active/`. Parked, not dispatchable. | Something worth keeping arrives that nobody has decided on. | Promoted by writing it into an RFC the operator approves (then it leaves the queue and becomes plan-derived work). |

**Use the team's own workflow where it has one.** Most Linear teams already
have a review state and a backlog state, and a factory that ignored them would
put a second vocabulary on a board people already read. `factory init` resolves
both from the team's real states; a team with no equivalent gets the label
instead, and nothing else changes.

**Blocked is always a label**, because no default Linear workflow has a state
for "waiting on a human decision" and the issue still has to say where the work
itself stands. A label sits alongside the state rather than replacing it.

**Single-select discipline.** An item is in at most one queue at a time. `rfc`
is orthogonal and marks what an issue *is*, not what it is waiting for.

**The one state the factory never writes** is `linear_approved_state`
([`approvals.md`](approvals.md)). Every other transition is bookkeeping it owns
and is expected to make. That single exclusion is the whole boundary, and it
holds because Linear's MCP cannot create workflow states: the marker carrying
the decision had to be handed to the factory rather than minted by it.

**The ASK line.** A `blocked` issue must carry a comment whose first line is
`ASK: <one-line question>` — the specific decision the operator has to make.
It is **edited in place** as the ask sharpens, and a second comment means the
question itself changed. Four comments for one yes/no is the factory being
loud, not thorough. Consumers read the latest.

## The other axes

- **Machine work is not here.** Unblocked, dispatchable work is whatever the
  plan document says is left, recomputed every beat. The greenlight path for
  new work is always an RFC issue the operator moves into
  `linear_approved_state`; the gaffer commits it to `plans/active/`, the
  watermark moves, and dispatch follows from the plan. An idea that arrives as
  an issue is promoted by writing it into an RFC, never by labelling it and
  dispatching off it.
- **`agent-ready` / `agent-working`** (GitHub, PR-level) — the operator hands
  a pull request they authored to the factory (`agent-ready`); pickup swaps it
  to `agent-working`. These stay on GitHub because they mark a pull request,
  not an ask. Verified completion follows the self-merge grants in
  [`autonomy.md`](autonomy.md); a `blocked` issue in Linear still routes to
  the operator, so attention ordering is unchanged.
- **no label** — in flight (a worker is on it) or done.

## The priority rule

Each beat, the loop spends the operator's attention in this order (encoded in
[`factory-loop.md`](factory-loop.md)):

1. **Ready for Testing first** — verify and surface; these are one action from
   shipping. The WAITING ON YOU block leads with this section, its top item
   marked `⏭`.
2. **Blocked** — surface in the block with the specific ask (decision or
   `[human step]`) and a direct URL.
3. **Dispatch the next plan steps** — machine work, highest-leverage plan
   first, straight off `plans/active/` and never through the tracker.
4. **Backlog is parked** — report depth only; never dispatch. Promote only via
   an RFC the operator approves.

The queues order **the operator's** attention. The reception → gaffer channel
(`~/.factory/inbox/<instance>/`) orders **the gaffer's** — the two are
orthogonal.

## Viewing them

`list_issues team=<linear_team> state=<…>` or `label=blocked`, through the MCP,
with the calls and the payload discipline in the
[`linear`](../.claude/skills/linear/SKILL.md) skill. For a person, the team's
own board already does it: the queues are the columns, which is the point of
using the team's states rather than a private vocabulary.

The scope wall that `factory-queues.sh` enforced is the team. One factory, one
Linear team — a factory reads its own team and no other, the same way it works
inside `repo_scope` and leaves the rest of the account alone.

## Rollout

Each instance ensures the labels it actually needs — `rfc` and `blocked`
always, plus `testing` or `backlog` only on a team whose config named no state
for them — with an idempotent `create_issue_label` in its tending beat.
Per-instance, never mass-created across a workspace.

No state is ever created, because none can be: `factory init` reads the team's
states and asks which ones the operator means.
