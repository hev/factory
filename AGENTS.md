# AGENTS.md — working in hev/factory

This repo is **the machine**: the loop, the floor, the picker, the `factory`
CLI, and the contracts they run on. Public, Apache-2.0.

## What does not belong here

Nothing about one estate. No hostnames, no vault references, no Slack webhooks,
no GitHub accounts, no provisioning. Those live in the private overlay
(`hev/factory-pro`), which drops files into a checkout of this repo rather than
forking it. The dependency runs one way: **this build never learns the overlay
exists.**

The seams the overlay uses — `identity/<role>`, `notify/send` — are documented
in `docs/extending.md`, and the public build is **required to keep them
satisfiable by hand**. An executable that prints a token on stdout is the whole
contract. If a change here would make that only workable with provisioning
someone has to buy, it is the wrong change.

## Contracts are the product

`contracts/` is normative, not documentation. The gaffer is a Claude session
handed `factory-loop.md` and told to follow it exactly; the desk is handed
`reception-charter.md`. **Behaviour changes by changing the contract**, and a
script that quietly does something the contract does not describe is a bug even
when it works.

Read `contracts/README.md` before editing any of them.

## The two approval doors

Set by whether `linear_team` is present in `factories/<name>.toml`. This is the
single most load-bearing config decision and both paths are supported:

- **Linear** — the operator moves an RFC issue into `linear_approved_state`;
  the gaffer commits the plan to `plans/active/`. Queues are Linear states and
  labels. One tap from a phone, and what setup recommends.
- **A merged pull request** — no `linear_*` block. The operator merges the PR
  that adds `plans/active/<slug>.md` to `plans_branch`, and that merge is the
  entire signal. Queues become `plans/blocked/` and `plans/backlog/` files.

`contracts/approvals.md` has the trade, including why protecting `plans_branch`
turns "the gaffer never writes `linear_approved_state`" from a rule it follows
into a 403 it cannot lift — and what that costs on the factory's own
bookkeeping commits.

**One factory, one team.** `linear_team` is the scope wall in Linear that
`repo_scope` is on GitHub. Two factories pointed at one team read each other's
board and dispatch against each other's RFCs.

## home_host

`factory-up.sh` and `factory-iterate.sh` refuse to boot anywhere but the
configured `home_host`, compared as `hostname -s`, lowercased. Two parents
against one plan source produce duplicate dispatch, and a stale clone on a
laptop is the usual way that happens. Do not weaken this guard for convenience;
`HOST_OVERRIDE` exists for the cases that need it.

## Where this runs

Developed on a laptop, run on a headless host. Nothing is edited on the host —
changes land here, get pushed, and reach the box through the overlay's
`host/update.sh`. A gaffer holds its loop contract in context from the moment
it starts, so **a pulled contract does nothing until the gaffer restarts**.

## Shell conventions

`set -uo pipefail` is standard here, which makes `cmd | grep -q` a trap: grep
exits at the first match, the producer takes SIGPIPE, and the pipeline returns
141 — read as failure. Capture the output first, then match it.

Prefer a loud failure to a silent `exit 0`. A check that cannot fail reports
nothing.
