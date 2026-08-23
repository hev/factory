# plans/

The executable queue for this repo — the factory machinery itself. Directory is
state:

- **Queue** = an RFC issue in Linear nobody has approved yet. A proposal,
  never a mandate.
- **`active/`** = approved and loop-eligible. The operator approves the RFC in
  Linear ([`../contracts/approvals.md`](../contracts/approvals.md)) and the gaffer
  commits it here, which is what moves a gaffer's watermark.
- **`archive/`** = done, with a dated `> **ARCHIVED …**` stamp naming what
  shipped and where any tails live. Never deleted — provenance stays
  greppable.

Every factory has one of these in its own `plans_repo`; this one happens to
belong to the factory that builds factories.

## How to write one

A plan is a work list, not an essay. The gaffer re-reads it on every beat and
decomposes it into worker briefs, so what earns its place is what a worker
cannot guess. Work backwards from the person the work is for, in this order:

- **What changes for the user.** Who they are, and what they can do when this
  ships that they cannot do now. Write it before you know how it will be
  built. A plan that opens on the implementation has skipped the only part the
  operator can actually judge.
- **Why now**, in a sentence. What it costs them today, or what it unblocks —
  the reason to spend the factory on this rather than the next thing.
- **How success is measured.** The number, event, or observation that says it
  worked, and where you would go to read it. When nothing is measurable, name
  what you would watch instead and say plainly that it is a judgment call. A
  plan whose only definition of done is *the code merged* is measuring the
  factory, not the user.
- **The steps**, in order, each with an acceptance condition specific enough
  that a gaffer can tell whether it happened. A step nobody can check is not
  a step.
- **Constraints and links** a worker would otherwise guess at: the
  authoritative spec, the interfaces to hold still, what is deliberately out
  of scope, the learnings that already rule an approach out.

**Budget: 120 lines.** A plan that does not fit is either two plans, or one
plan with a directory (`plans/active/<plan>/`) holding the fixtures and specs
that made it long. Everyone pays to read this file — the operator deciding
whether to approve it, and the gaffer on every beat until it archives.

Send it back when the first thing on the page is a design and the user is
somewhere on page two, or when nobody can say what would be different after it
ships. Both are cheaper to fix in the queue than after a worker builds them.

Cut on sight:

- Background that restates the title, and a summary that previews the design
  above the design.
- Alternatives considered. One line per rejected option, or nothing.
- Prose reciting what the step list already says.
- "Here's why", "at its core", "it's worth noting", "not just X — it's Y",
  and any sentence whose subject is the plan itself.
- A second em-dash in the same sentence, and an adjective standing where a
  number belongs.

Precision is not verbosity. Keep every acceptance condition, link, version,
and known failure, even when they make the document longer.
