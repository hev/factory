# Internal docs

Operator notes for the machine that runs the fleet. The customer-facing story
is [`../README.md`](../README.md); everything here is the layer beneath it,
organized by which of the three components it belongs to.

**Normative first.** [`what-is-a-factory.md`](what-is-a-factory.md) is the
definition every other document in this repo should be explainable in terms of.
When an embodiment question comes up ("can we swap tmux for pods?"), that file
is the acceptance test.

## Setup

Onboarding is a conversation, not a form. `./factory` boots reception, and on a
machine with no factory configured `reception-up.sh` tells it so in its boot
prompt — it runs the `init-factory` skill
([`../.claude/skills/init-factory/`](../.claude/skills/init-factory/)) and
walks you through the first one.

`./factory list` says what is configured here, and `./factory cleanup <name>`
removes one completely — sessions, worktrees, state, config, watermark — after
showing every path and asking. Underneath the conversation is `./factory init`,
a non-interactive subcommand
that validates the answers and writes `factories/<name>.toml`. Reception calls
it; so can you. `factories/example.toml` documents every field it writes.

The split is deliberate: working out what a factory is *for* benefits from an
agent, and remembering TOML field names does not.

## The picker

- [`child-ledger.md`](child-ledger.md) — the JSON the gaffer writes per live
  worker and the picker reads to label rows with instance, issue, plan, and the
  stale flag. The one contract between the two components.

## Reception

Reception's own contract is [`../reception-charter.md`](../reception-charter.md),
which is the whole specification: voice, transcript, first-run setup, the
message tiers, when it may speak first, and the hard lines.

- [`events.md`](events.md) — the spool the floor talks to the desk through:
  what a worker says when its state changes, what the gaffer already said
  outward, and the one flag that separates the two. The reason reception can
  answer *why is that one stuck* without capturing a single pane.

## The gaffer

- [`queues.md`](queues.md) — the three Linear triage labels, and why the
  tracker is a human surface: machine work lives in the plan document and
  never becomes an issue.
- [`approvals.md`](approvals.md) — how an RFC gets approved: one workflow state
  in Linear, why prose stopped counting, and why the commit into
  `plans/active/` is bookkeeping. `[contract]` is still yours.
- [`autonomy.md`](autonomy.md) — the per-gate-class self-merge ledger: what
  this gaffer may merge without you, and what revoked it last. Everything
  starts off. Distinct from approvals, which delegate nothing.
- [`learnings.md`](learnings.md) — the part that compounds: what a worker is
  allowed to leave behind for the next one, where it lives, and the bar it has
  to clear so the store does not become litter.

The gaffer's contract is [`../factory-loop.md`](../factory-loop.md), and how it
is run depends on the instance's `runtime`:

- **resident** — `../factory-up.sh` keeps a claude session alive in tmux and
  the agent schedules its own beat.
- **one-shot** — `../factory-iterate.sh` runs one iteration as `claude -p` and
  exits, with `../one-shot-addendum.md` appended to the contract to override
  the parts that assumed a resident session. Liveness is
  `../scripts/factory-health.sh`.

## The repo itself

- [`extending.md`](extending.md) — the three places a factory calls out to
  something it does not ship, and the one that is prose rather than code.
- [`../plans/`](../plans/) — the executable queue for this repo.
