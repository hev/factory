# Approving an RFC

A plan enters the factory when it lands in `plans/active/` on `plans_repo`.
That has not changed and is not going to. The watermark is the sensor, a plan
on the branch is the intent, and everything downstream reads from one branch:
decomposition, dispatch, archive. Which branch is `plans_branch` in
the factory's config, frozen there by `factory init` and `main` unless
somebody said otherwise.

What changed is **where the decision is made**. There are two doors onto that
branch and a factory has exactly one, decided by whether `linear_team` is in
its config:

- **Linear** — the RFC is a Linear issue, the operator's act is a state
  transition on it, and the commit into `plans/active/` is bookkeeping the
  gaffer does on their behalf. This is the rest of this file, and it is the
  better of the two, because it is the one that works from a phone.
- **A merged pull request** — the RFC is the plan file itself, the operator's
  act is merging the pull request that adds it, and there is no bookkeeping
  because the merge *is* the commit. See "The other door" below.

Everything downstream of the branch is the same either way. The doors differ
in where the operator stands, not in what the factory does afterwards.

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

## The other door: a merged pull request

A factory with no `linear_team` has no board to read, so the operator's act has
to be something visible on the branch itself. It is this:

**The operator merges a pull request that adds `plans/active/<slug>.md`.**

Reception drafts the plan and opens that pull request
([`reception-charter.md`](reception-charter.md)); the gaffer opens one when it
has an RFC to propose. Neither merges it. The next beat's watermark sees a new
plan on `plans_branch` and dispatch follows exactly as it always has — step 1a
is skipped, 1b is unchanged, and nothing downstream can tell which door the
plan came through.

**Why not "commit the plan file"?** Because reception has repo access and can
commit, and a signal the machine can perform is not a signal. The merge is the
one act nothing in the factory performs.

**Branch protection is what makes that structural.** Say it plainly rather than
implying the boundary holds by itself: on an unprotected `plans_branch`, "the
factory never merges into it" is a convention of exactly the same class as
"the gaffer never writes `linear_approved_state`" — a rule it follows, and
crossing it is a factory defect rather than an impossibility. Switch on branch
protection requiring a pull request, and it becomes a 403.

That switch is the operator's, and it has to be: a factory that could set it
could unset it. The first-boot preflight reports whether it is on and then
stops mentioning it.

**And protection is a trade, not a free win.** "Require a pull request before
merging" blocks *every* direct push to the branch, not only the ones that
matter — so the gaffer's own bookkeeping goes with it: the archive commit that
retires a finished plan, and the `plans/blocked/` and `plans/backlog/` files in
[`queues.md`](queues.md). Under protection those become pull requests somebody
has to merge, which is operator attention spent on the factory tidying up after
itself, and that is the thing this whole rig exists to stop.

The rule that would fix it — restrict pushes touching `plans/active/**` and
leave the rest of the tree alone — is a GitHub **push ruleset**, and push
rulesets need Team or Enterprise. On a personal repo on the free plan it is not
available, so do not recommend it as the answer.

Which leaves an honest two-position setting rather than a switch that is
obviously right:

- **Off** (the default, and what `factory init` writes). Everything
  direct-commits, onboarding costs nothing, and "the factory never merges your
  plan in" is a rule it follows — the same class as `repo_scope`, and crossing
  it is a factory defect rather than an impossibility.
- **On.** The plan door is a 403. The price is that the factory's bookkeeping
  needs merging, and the durable fix for *that* is `identity/gaffer`
  ([`extending.md`](extending.md)): a second account with push access to the
  branch and no merge rights on it, at which point both halves are mechanical
  at once. That is the same escape hatch item 3 below names for Linear, and it
  is the same one file away.

Say which position a factory is in rather than implying it is in the second
one.

**Conditions are still comments** — review comments on the pull request, read
exactly as steering is read in Linear. The gaffer amends the plan file with a
commit on the branch and replies saying what changed; the pull request stays
open until the operator merges it. `looks good but drop the cache step` is not
an approval and an approving review is not one either. **The merge is the whole
of it.**

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

In pull-request mode the same three collapse into one fact with the same
shape: the factory opens pull requests and never merges into `plans_branch`,
and item 3's mechanical version is branch protection rather than a second
account. It is one toggle away instead of one file away, which makes it the
easier of the two boundaries to actually enforce.

Reception is under the same rule and needs it stated separately, because
reception *does* write to Linear: it drafts RFC issues, which is most of what
it is for. It never sets state. That the two acts are different fields is what
makes reception's Linear access safe when its GitHub access never was —
a comment reception writes is indistinguishable from the operator's, and a
state transition is one nobody has to interpret.

## Reading the state

Linear mode only; a factory without a team reads nothing here, because the
branch it already fetches is the whole of its reading.

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
