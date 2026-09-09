# Every dollar of inference lands on a line, an RFC and a merge: the kit dashboard shows cost per line, cost per Linear issue, what shipped, and what the subscriptions saved over API

> Source: https://linear.app/hevmind/issue/FAC-18/every-dollar-of-inference-lands-on-a-line-an-rfc-and-a-merge-the-kit

As the operator paying for the factory, I want every session's inference cost attributed to the line, the Linear issue and the pull request it served, so that I can say what an RFC cost to ship, which line is expensive, and how much running on subscriptions saves against API prices.

## Why now

Today the answer to "what did this cost" is a shell script over `~/.factory/beats/*.jsonl` and `~/.claude/projects`. On 2026-09-08 that script found the three gaffers were ~$123/day at API prices and the factory instance was firing 130 beats a day on its own comments; nobody had seen it because nothing showed it. The kit dashboard (<issue id="03739a32-96fb-4326-85b2-0ba1e4ebc4d2" href="https://linear.app/hevmind/issue/FAC-9/kit-dashboard-one-row-per-trace-search-and-filters-run-in-layer-marks">FAC-9</issue>, <issue id="d3424659-9cb7-40bd-b6b8-dd527137afa7" href="https://linear.app/hevmind/issue/FAC-11/kit-dashboard-loads-in-a-second-and-says-so-filters-without-a-search">FAC-11</issue>) already has one row per trace with tokens and cost and a stats window, but codex sessions arrive with no model and no cost (`session_cost_usd: null` on every codex row in `evals.jsonl`), nothing ties a row to an instance, a plan step, an issue or a PR, and there is no notion of a subscription, so the number it shows is an API price nobody pays. The mini is meant to be sold as a package priced by its subscriptions ($400–600/month); that pitch needs this screen.

## Acceptance criteria

1. Open the kit dashboard the mini serves and choose the factory view: a table with one row per line (`factory`, `lyr`, `charlie`) for the last 7 days showing beats, worker sessions, tokens, API-equivalent dollars and subscription dollars, split by role (gaffer, worker, foreman, eval, labeller, other).
2. Tap any Linear issue in that view, e.g. <issue id="d7732a3c-1ef6-4e80-9aca-30df3c26b641" href="https://linear.app/hevmind/issue/FAC-13/the-mini-comes-back-from-a-hard-restart-unattended">FAC-13</issue>: cost to date at both prices, sessions by role, the PRs it produced with merged-at, and whether the issue is Done. An issue that is Done shows a single "cost to ship" number.
3. A shipping panel for the window: issues reaching Done, PRs merged, deploys that landed (Cloudflare Pages for travelswithcharlie, CI for the rest), and dollars per merge.
4. An arbitrage tile: "This week: $X at API prices for $Y of subscriptions, saved $Z", with each plan's share of its weekly limit (claude max, codex, and any plan added in config, groq included) so that 77% on Tuesday is visible before it is a problem.
5. Every session with source evidence carries its recorded model, tokens, instance and role; worker rows carry their issue when an exact source join exists. Sessions lacking tokens, model or issue remain visible and explicitly incomplete; never invent tokens or issues. The tile states incomplete coverage in one line.
6. The promo loops and any other session on the same subscriptions appear under role `other`, so the arbitrage number is what the plans actually carried, not what the factory alone did.

## How to test it

* Dashboard: the `hev serve` the mini runs under `com.hev.serve`, over Tailscale from the laptop; the factory view is a tab beside the traces.
* Criterion 5: inspect `hev trace list --harness codex --since 7d --json` and the factory view: source-backed rows retain their recorded usage and attribution, missing values remain explicitly incomplete, and the tile states the coverage gap without fabricated tokens or issues.
* Criterion 4 against ground truth: compare the claude plan share with `claude` usage on [claude.ai](<http://claude.ai>) for the same week; within 10 points.
* Criterion 2 on a closed issue: <issue id="03739a32-96fb-4326-85b2-0ba1e4ebc4d2" href="https://linear.app/hevmind/issue/FAC-9/kit-dashboard-one-row-per-trace-search-and-filters-run-in-layer-marks">FAC-9</issue> shows the sessions the child ledger and harvest recorded for it, and the merge dates match `gh pr view`.
* Backfill check: `jq -c -f evals/layer-row.jq ~/.factory/evals/evals.jsonl | hev eval put` still replays without duplicates.

## Steps

1. **Codex sessions become trace rows.** A reader for `~/.codex/sessions/**/rollout-*.jsonl` (the `token_count` events carry input, cached, output and reasoning tokens; `session_meta` carries cwd, model and thread id) producing the same trace shape claude sessions use in kit. Backfill 30 days. *Accept:* criterion 5.
2. **Attribution on every row.** `scripts/factory-as.sh` already exports the role and `FACTORY_INSTANCE`; the child ledger (`~/.factory/children/*.json`) carries plan, step, issue and issue_url per worker session; harvest stamps carry the PR. Join them onto the trace row at ingest, and mark anything else on the machine `other` with its cwd. *Accept:* criterion 5, criterion 6.
3. **Two prices.** `prices.toml` in kit: per model, list price per million input, cached and output, dated so a price change does not rewrite history. `subscriptions.toml`: per plan, monthly price, weekly reset day, which harness and models it carries. Each row gets `api_usd` from the table and `sub_usd` as its token share of the plan's spend in that week. *Accept:* for claude sessions `api_usd` matches the harness's own `total_cost_usd` within 5%.
4. **Outcomes.** Per issue in `linear_team`: state and Done date from Linear, PRs and merged-at from GitHub, deploy landed-at from the repo's CI or Pages. Cached, refreshed hourly. *Accept:* criterion 2, criterion 3.
5. **The screens.** Per-line table, per-issue page, shipping panel, arbitrage tile, on the existing dashboard, behind the same filters and window. *Accept:* criteria 1–4, with a screenshot on this issue.
6. **The number in the beat.** The gaffer's status report already carries tokens; add `api_usd` and `sub_usd` to the beat line and the report so the picker's day column can show them. *Accept:* `~/.factory/beats/<instance>.jsonl` rows carry both fields.

## Constraints

* Ingestion and attribution live in this repo (hev/factory), the dashboard and price tables in hev/kit; nothing in either learns the overlay exists. Prices and subscription config are plain files an operator edits by hand.
* Subscription cost is an allocation, not a measurement: the plan costs the same whether the week is 10% or 100% used, and the tile must say so in one line rather than pretend to precision. Use the totals in `subscriptions.toml`; a missing price or Codex tier is an operator-editable placeholder labelled as such. The operator will fill the real monthly figures and tier; missing inputs do not block finishing the drafts, and savings must not imply an unknown bill is zero.
* Rate-limit share per plan is read, not estimated, and both sources are validated (2026-09-08): Claude from `GET https://api.anthropic.com/api/oauth/usage` with the Claude Code OAuth token from the login keychain (`limits[kind=weekly_all].percent` and `resets_at`; the keychain is readable from a gui-domain launchd job on the mini, not over ssh), codex from the `rate_limits` object on every rollout's `token_count` event (`primary.used_percent`, a 10080-minute window, `resets_at`). A plan with neither gets tokens against a configured weekly basis, labelled as an estimate.
* Out of scope: budgets that stop the line, per-token alerting, anything that posts to Slack. The foreman reads the same rows and can say "lyr cost $40 yesterday" on an idle floor once these exist.
* Learnings that rule things out: the beat ledger records `cost_usd` 0 for codex because a subscription prints no price; do not "fix" that upstream, the price belongs in step 3.


## Handoff and remaining acceptance — 2026-09-09

Draft source is in https://github.com/hev/factory/pull/18 and https://github.com/hev/kit/pull/32. Runnable ingestion, exact attribution joins, scoped outcome refresh and pricing export have fixture and isolated-data evidence; no criterion is declared complete from fixtures alone.

- **Steps 1–2:** complete source-backed attribution and backfill, keeping missing usage/identity records explicitly incomplete under the operator’s 2026-09-09 answer. Do not repeat the exhausted legacy assignment search or invent data. Acceptance is amended criteria 5–6 and visible coverage gaps.
- **Step 3:** use subscriptions.toml totals with labelled operator-editable placeholders for missing prices/tier; missing bills no longer block draft completion. Verify historical effective rates and complete same-session usage against exact harness cost records; report incomplete source counters honestly. Retain the 5% comparison for comparable complete records and same-week plan-share check; never invent price precision.
- **Step 4:** verify complete historical issue/merge/deploy coverage using only configured team/repository scope; arrange the hourly fresh issue snapshot and refresh after source rollout. Exact merge-SHA associations do not establish all prior commits shipped in a later release.
- **Step 5:** after source approval and authorized deployment, independently exercise all four deployed views, closed-issue interaction and both prices; capture the required evidence. Local screenshots are a stand-in, not deployed acceptance.
- **Step 6 and backfill:** after the operator merges the combined contract and authorizes rollout, verify new beat/report fields. Resolve the absent public replay adapter using the repository's supported ingestion path and prove duplicate-free replay; never rewrite historical beat lines.

All six original steps remain open until their acceptance passes. Private session audits and billing inputs remain machine-local.
