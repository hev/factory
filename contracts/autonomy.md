# Autonomy ledger

What this factory's gaffer may merge without the operator, per gate class.
This file is the ledger and the grant: editing it is how autonomy is given and
how it is taken away. The gaffer reads it every beat and reports its delta in
status reports.

## Current status

Everything starts **OFF**. A factory you just stood up has no track record, and
a gaffer that merges on its first day is asking to be trusted before it has
been watched. Turn a class on when the merges it has been opening have stopped
being interesting to read.

| class | self-merge | condition | streak | last incident |
|---|---|---|---|---|
| `[impl]` | **OFF** | — | 0 | — |
| `[docs]` | **ON** (granted 2026-09-03) | quiet period — the block goes out, CI green, merges if the operator has not commented within 24h | 0 | — |
| plan lifecycle | **n/a — never a pull request** | the operator's state change *is* the decision ([`approvals.md`](approvals.md)) | — | — |
| `[contract]` | **OFF — operator-only, always** | the merge *is* the decision | — | — |

Hard lines, never self-merged regardless of streak: outbound communications,
payments, publishing, and secrets or identity changes.

**The plan lifecycle is not on this ledger, and never will be.** A gaffer
committing an approved plan is not exercising autonomy — it is recording a
decision the operator already made, in a beat that then reads the result back
off the plans branch like any other. There is no streak to build and no grant
to revoke, because nothing was delegated. What *would* belong here is a gaffer
committing a plan whose RFC nobody moved into the approved state, and that is
not a grant this ledger can issue: it is the intent boundary, and it is in
[`what-is-a-factory.md`](what-is-a-factory.md) rather than here for exactly
that reason. See [`approvals.md`](approvals.md) for the mechanism and for the
three things standing between a one-account factory and a self-approving one.

## How a class is granted

Flip the cell to **ON**, name the condition, and date it. The conditions worth
using, in the order they are usually earned:

- **`[impl]`** — the factory review pass is done (acceptance evidence checked
  against the brief, a "factory verified" comment posted) and CI is green.
  Grant this after enough of its pull requests have merged unchanged that
  reading them has become a formality.
- **`[docs]`** — a quiet period. The block goes out, and the pull request
  merges if the operator has not commented within 24 hours. Silence is consent,
  with an explicit window.

## How a class is revoked

**A revert or a correction on a self-merged pull request flips that class OFF
immediately**, and it stays off until 10 consecutive clean merges rebuild it.
Record the flip and the streak here — the count is the evidence, and a streak
nobody wrote down is a story rather than a record.

Revocation is not a punishment and it does not need a conversation. It is the
mechanism working: the operator's control over a self-merging factory is the
post-merge window, and using it is how the window stays real.

## Access is not permission

This ledger says what the gaffer may **merge**. It says nothing about what the
account can **reach**, and on this build those are far apart: one GitHub
account carries every role's access, so a gaffer can technically do most of
what the rows above withhold. The gap is held by convention, the same way
`repo_scope` is. The loop contract leans the other way on everything outside
these rows — check your access and act on it rather than filing a blocker
about a thing you could have done ("Access — check it, then use it").

Two seams close the gap when the stakes rise, and both exist today:

- **An account per role.** Drop an executable at `identity/<role>` and the
  gaffer acts as an account that is not yours, scoped to what it should hold
  ([`extending.md`](extending.md)).
- **Branch protection on `main`.** It turns the OFF rows above into a 403,
  and a rule the server enforces does not depend on the gaffer reading this
  file correctly.

Until both are in place, the honest statement is that this ledger is a promise
the factory keeps rather than a permission it is denied.

## Self-merge log

One line per self-merge: date, instance, class, the pull request, and whether
it stayed clean. Append as they happen.

<!-- 2026-01-01 · acme · [impl] · acme/api#12 (title) — factory verified, CI green, no comment in the interim. Clean. -->
