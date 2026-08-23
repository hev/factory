# Approving an RFC

A plan enters the factory when it lands in `plans/active/` on `plans_repo`.
That has not changed and is not going to. The watermark is the sensor, a plan
on the branch is the intent, and everything downstream reads from one branch:
decomposition, dispatch, archive. Which branch is `plans_branch` in
the factory's config, frozen there by `factory init` and `main` unless
somebody said otherwise.

What changed is **where the decision is made**. The RFC is a Linear issue, the
operator's act is a state transition on it, and the commit into
`plans/active/` is bookkeeping the gaffer does on their behalf.

## Why Linear

The merge button was never the decision. It was the decision *plus* getting to
a machine that could press it. Every RFC the operator had already read and
agreed with sat in the queue until they were somewhere they could open GitHub
properly, and a queue that stalls on posture rather than judgment is the
"merge friction is a rules bug, not an operator duty" rule pointed at the one
place it had not been applied yet.

Approving from a phone, in one tap, on the way somewhere is the point. So the
operator works where a phone works and the machine works where code works.
Linear holds the RFC, the asks, and the queues. GitHub holds branches, pull
requests, and CI, none of which is ever waiting on a person.

## The signal

**The issue moves into the approved state. That is the whole of it.**

Which state is `linear_approved_state` in the factory's config, resolved at
`factory init` from the states the team actually has. Nothing else approves an
RFC: not a label, not a comment, not `lfg`, not the operator saying yes out
loud to reception.

Prose used to count, and dropping it is the one place this got stricter rather
than looser. A judged approval is the gaffer deciding that a sentence means
yes — on an account whose comments the gaffer can also write. One field that
the factory is structurally not the author of is worth more than a paragraph
of good intentions about how it reads sentences. It also costs nobody a beat:
there is no ambiguity to post an `ASK:` about and no reading to state, because
there is no reading.

The operator taps the state on a phone in about a second, which is less work
than typing `lfg`.

## Conditions are comments

`looks good but drop the cache step` is not an approval, and under the old
rules it was. It is **steering**, and it is read as steering: the gaffer
applies it to the RFC description with a surgical edit, replies on the comment
saying exactly what changed, and the issue stays where it is.

The operator then moves the state, or does not. Comment and state in either
order is fine — the gaffer applies every unapplied comment before it commits
the plan, so an approval that arrives first still gets the amended document.
What it will never do is dispatch against a version the operator asked it to
change.

This is the same ordering rule as before, with the ambiguity taken out: the
condition lands in one channel and the decision lands in another, and neither
one can be mistaken for the other.

## What the gaffer does on approval

1. Reads the approved RFC issues for its team.
2. Applies any comment it has not already applied.
3. Commits `plans/active/<slug>.md` onto `plans_branch` — the issue
   description, under a header naming the issue URL, so the plan on disk says
   where it came from.
4. Moves the issue on to a started state and says nothing about it. The
   dispatch line in Slack is the answer the operator gets.

Step 3 is why the rest of the machine is untouched. The watermark still moves
because a file appeared on the branch, workers still read the plan document,
and a plan somebody writes into `plans/active/` by hand still works exactly as
it always did.

## First run commits nothing

A beat with no `.factory-watermark` file commits no approvals, however many
are sitting there. The watermark is recorded from the branch *after* the
approval sweep, so a plan committed on that beat would land inside the
watermark and be dispatched never. That is silent loss, and it is worse than
the mass-dispatch the first-run rule already exists to prevent.

So the first beat reports which issues are approved and commits none of them.
The second beat has a watermark, commits them, and dispatches. One extra beat,
nothing dropped.

## This is not an inbound control plane

"Nothing commands a factory over the network" is a standing invariant, and an
approval does not cross it. The gaffer polls Linear outbound on its own beat
and finds a state sitting there — the same shape as an answer to an `ASK:`
line, which has steered the loop since the beginning. Nothing is pushed at the
factory, no port is open, and the beat still decides when to look.

## What this does not extend to

**RFCs only.** The signal moves plan lifecycle: a new plan into
`plans/active/`, and the archive commit that retires it.

`[contract]` pull requests — changes to `factory-loop.md`, changes to how the
factory itself operates — are still merged by the operator, by hand, on
GitHub, always. The friction there is doing its job: a factory that can talk
its way into rewriting its own contract has no contract. `[docs]` and `[impl]`
are governed by the self-merge grants in [`autonomy.md`](autonomy.md), which
is a separate mechanism and stays separate.

## The honest part

A factory runs as one GitHub account and one Linear login — the operator's —
so `save_issue` can set any state the operator can. Three things stand between
that fact and a factory that approves its own RFCs:

1. **The gaffer never moves an issue into `linear_approved_state`.** Every
   other transition is bookkeeping it owns. This is a convention it follows,
   the same class as `repo_scope` — and like `repo_scope`, crossing it is a
   factory defect, not a grey area.
2. **The marker is one it could not have minted.** Linear's MCP creates labels
   and cannot create workflow states, so the one marker that carries the
   decision had to be handed to the factory. The states it *does* write — in
   progress, review, done — are the team's own, already there before it
   arrived, and none of them means yes.
3. **`identity/gaffer` makes it mechanical.** Configure the identity hook
   ([`extending.md`](extending.md)) and give the gaffer's Linear login a role
   that cannot write that state, at which point the boundary is a permission
   error rather than a promise. This is the strongest version of it, and it is
   one executable file and one Linear role away.

Reception is under the same rule and needs it stated separately, because
reception *does* write to Linear: it drafts RFC issues, which is most of what
it is for. It never sets state. That the two acts are different fields is what
makes reception's Linear access safe when its GitHub access never was —
a comment reception writes is indistinguishable from the operator's, and a
state transition is one nobody has to interpret.

## Reading the state

There is no script. Linear is read through the MCP, and the calls are in the
[`linear`](../.claude/skills/linear/SKILL.md) skill — load it before the first
one. What was `factory-approvals.sh` is now:

```
list_issues team=<linear_team> label=rfc state=<linear_approved_state>
```

Two states matter and both are deterministic, which is the point of dropping
prose: an RFC is approved or it is not. Anything with comments the gaffer has
not applied is steering it owes a reply to, and that is a different question
from whether it may build.
