# Arm a fresh factory on its first beat

Draft. Not in `plans/active/`, so nothing dispatches against it.

## What changes

Somebody who has just stood up their first factory approves their first RFC and
gets a worker dispatched on the next beat. Today that approval costs two beats
and the first one reports back having done nothing, at the one moment the
operator has no track record to weigh it against and is deciding whether the rig
works at all.

## Why now

Setup is the only time a factory is judged entirely on its first ten minutes,
and the current cold start spends them proving nothing.

## How success is measured

Time from the first approval to the first beat line carrying `dispatched=1`,
read from the beat log. One beat afterwards, bounded by `interval_base`: 300s
at the default. Today it is two beats, 600s, and three when the approval landed
before launchd got round to booting the gaffer.

The guard has to survive: a factory booted against a `plans_repo` that already
has plans in `plans/active/` still dispatches zero workers on its first beat.

## The ordering, which is the whole change

The first-run suppression is an artefact of when the watermark is written. Step
8 records the SHA *after* 1a has committed, so a plan committed on a first beat
lands inside the watermark and is dispatched never. That is silent loss, and
suppressing 1a is the current defence.

Record the watermark before 1a instead and both hazards close at once. Plans
already on the branch sit inside the recorded SHA and do not dispatch. Plans
committed this beat land after it and do.

## Steps

1. **Capture the first-run watermark before 1a runs.** In
   `contracts/factory-loop.md`, add a preflight to step 1 for a beat with no
   `.factory-watermark`, or one whose recorded branch is not `plans_branch`:
   `git fetch origin <plans branch>`, write `<branch> <sha>` to the file
   immediately, then continue into 1a unchanged.
   *Done when:* the file holds the pre-1a SHA before any approval is committed,
   and a beat killed between 1a and step 8 leaves a watermark the next beat
   reads as an ordinary beat rather than another first run.

2. **Delete the 1a suppression.** Remove *First run suppresses 1a's commits
   too* from 1b. A first beat commits approved RFCs the way every other beat
   does.
   *Done when:* no rule in `factory-loop.md` makes a first beat commit fewer
   approvals than a later one.

3. **Rewrite `approvals.md` to match.** Its *First run commits nothing* section
   states the rule being removed, and a contract that contradicts the loop is
   worse than either version alone.
   *Done when:* the section describes the pre-1a capture and the invariant it
   protects, and no longer promises an extra beat.

4. **Restate the first-run rule as what is left of it.** The mass-dispatch
   guard stops being a special case: plans already in the tree are inside the
   watermark, so the ordinary diff dispatches nothing without being told. Keep
   the inventory report, which is the operator's evidence that the factory read
   the branch.
   *Done when:* a gaffer booted against a plans repo holding 3 active plans
   dispatches 0 workers on beat 1 and names all 3 in its report.

5. **Branch changes take the same path.** A recorded branch that is not
   `plans_branch` gets the same preflight, for the same reason.
   *Done when:* editing `plans_branch` and running a beat records the new
   branch's pre-1a SHA, dispatches nothing from the tree already there, and
   dispatches an RFC approved and committed on that beat.

6. **Boot the instance `factory init` just wrote.** Init writes the TOML and
   returns, so the gaffer waits on launchd's `StartInterval`, up to 300s before
   the first beat exists. Have init call `factory-up.sh <name>` and
   `reception-up.sh <name>` on success, behind `--no-boot` for scripted use.
   *Done when:* `./factory init …` on the config's `home_host` leaves
   `gaffer-<name>` live in tmux with no timer involved, and on any other host
   boots nothing (`factory-up.sh` already refuses, so this is one check that it
   still reports cleanly rather than looking like a failed init).

7. **Fix the handoff.** The init-factory skill tells the operator that the first
   beat after a fresh boot dispatches nothing and the beat after it is the first
   that builds. Replace with the truth: approve it, and the next beat builds.
   *Done when:* `.claude/skills/init-factory/SKILL.md` promises one beat.

## Constraints

- Silent loss is what the current rule prevents, and it stays prevented by
  ordering rather than by suppression. Any version that records the first-run
  watermark at step 8 reintroduces it.
- The watermark file stays one untracked line, `<branch> <sha>`.
- Both runtimes. Step 1 is shared, and nothing here touches
  `one-shot-addendum.md`.
- Out of scope: `interval_base`, the pacing hint, and how fast beats fire. This
  removes a beat from the cold start; it does not make a beat shorter.

## Links

- `contracts/factory-loop.md:129` — 1b, the watermark and the first-run rules
- `contracts/factory-loop.md:475` — step 8, where the watermark is written today
- `contracts/approvals.md` — *First run commits nothing*
- `cmd/factory/init.go` — `runInit`, which currently writes and exits
- `launchd/com.hev.factory.plist` — `StartInterval` 300, `RunAtLoad`

## Rejected

- Arm on beat one only when `plans/active/` is empty: correct for a fresh plans
  repo, and leaves every other factory paying the tax for no reason.
- Shorten the first interval: makes the wasted beat arrive sooner.
