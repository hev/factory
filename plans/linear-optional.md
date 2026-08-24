# Linear becomes recommended, not required

Draft. Not in `plans/active/`, so nothing dispatches against it.

## What changes

Somebody evaluating this can run a factory without a Linear account. They
approve by merging a pull request that adds `plans/active/<slug>.md`, and the
rest of the machine behaves as it always has. Linear stays the better surface
and stays in the quickstart; it stops being the thing that has to happen before
anyone sees the rig work at all.

An existing Linear factory changes in no way. That is the harder half of this.

## Why now

Linear OAuth is the only prerequisite left that is not `brew install`, and it
gates evaluation on setup somebody has not yet decided is worth doing.

## How success is measured

A config with no `linear_*` fields boots both runtimes, and a plan merged into
`plans_branch` dispatches a worker on the next beat. Against that, `charlie` is
the regression test: it runs on Linear and its beats must not change.

## The approval signal, which is the part that is not obvious

"Commit the plan to `plans/active/`" does not work as the operator's act.
Reception has repo access and can commit, so a signal it can perform is not a
signal. **The operator merges a pull request that adds the plan file.** Nothing
in the factory merges into a protected branch, which is the role
`linear_approved_state` plays today: a marker the machine could not have minted.

Branch protection on `plans_branch` is what makes that structural rather than
conventional. It is the operator's to switch on, and the honest version of this
RFC says so out loud rather than implying the boundary holds without it.

`plans_branch` is still the only sensor. Both doors open onto it.

## Steps

1. **Stop requiring Linear at boot.** Remove `LINEAR_TEAM` and
   `LINEAR_APPROVED_STATE` from the required list at `factory-iterate.sh:108`;
   `factory-up.sh:100` already requires neither, and that asymmetry is the bug
   this step also fixes. Skip the MCP server injection at
   `factory-iterate.sh:259` when no team is configured, and say which mode the
   beat is in on the task line at `factory-iterate.sh:192`.
   *Done when:* a config with no `linear_*` fields boots under both runtimes,
   and a config with them produces a byte-identical task line to today's.

2. **Make the approval sweep conditional.** Step 1a of
   `contracts/factory-loop.md` reads Linear for approved RFCs. With no
   `linear_team`, skip it: the watermark on `plans_branch` is the whole sensor,
   which is what 1b already does.
   *Done when:* no beat on a Linear-less factory calls the Linear MCP, and the
   contract says which door is open in one place rather than in every step.

3. **Give the queues a home.** `contracts/queues.md` keys all three on Linear
   states or labels. Blocked and Backlog become `plans/blocked/` and
   `plans/backlog/` in the plans repo. Ready for Testing needs nothing: that
   queue is already an open pull request on GitHub, and queues.md says so.
   *Done when:* queues.md names both vocabularies and which applies when, and a
   Linear-less factory can park work and report it as blocked.

4. **Reception drafts, and cannot land.** `contracts/reception-charter.md` has
   reception writing RFC issues. Without Linear it opens a pull request adding
   the plan file instead, and never merges it.
   *Done when:* reception on a Linear-less factory produces a PR the operator
   merges, and nothing in the charter lets it merge its own.

5. **Let setup say no.** `factory init` validates none of those fields, so it
   already writes a Linear-less config without complaint. The work is in the
   `init-factory` skill, which currently says *Do not carry on without it*.
   That becomes a recommendation with the trade named: no phone approval, and
   branch protection is now yours to set.
   *Done when:* the skill can complete a factory with no Linear and says what
   was given up.

6. **Document both doors.** `contracts/approvals.md` is written as though
   Linear is the only one. It gains the pull-request door and the branch
   protection caveat, in the same voice as the existing honest part.
   *Done when:* approvals.md describes two signals and is explicit that one is
   structural only with branch protection on.

7. **Move it in the quickstart.** README and the site present Linear as
   recommended rather than required.
   *Done when:* somebody can read the quickstart, skip Linear, and reach a
   running factory.

## Constraints

- An existing Linear factory must be unaffected in behaviour, not merely in
  configuration. `charlie` runs on Linear and is the regression test.
- Branch protection is the operator's to set. A factory that could set it could
  unset it, which is the whole reason it is the boundary.
- The plans repo is operator intent, and step 3 puts machine bookkeeping in it.
  Keep `plans/blocked/` and `plans/backlog/` to one file per item with the same
  header shape as a plan, so the tree stays readable by diff.
- Out of scope: GitHub Issues as a Linear substitute, and any second approval
  mechanism beyond the two doors named here.

## Links

- `factory-iterate.sh:108` — the required list that refuses to boot
- `factory-iterate.sh:192`, `:259` — the task line and the MCP injection
- `factory-up.sh:100` — the resident list, which already requires no Linear
- `contracts/factory-loop.md` — step 1a, the approval sweep
- `contracts/queues.md` — three queues, all keyed on Linear
- `contracts/approvals.md` — the honest part, which this extends
- `contracts/reception-charter.md` — reception drafts RFC issues
- `.claude/skills/init-factory/SKILL.md` — *Do not carry on without it*

## Rejected

- Queues as GitHub issues: a second tracker for people who opted out of the
  first.
- Approval by commit rather than merge: reception can commit, so it is not a
  signal.
