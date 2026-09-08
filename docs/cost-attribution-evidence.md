# Cost-attribution review evidence — 2026-09-08

Authoritative [plan](https://github.com/hev/factory/blob/main/plans/active/factory-inference-cost-attribution.md).
This is an implementation handoff with **live acceptance still blocked**.
No deployment, merge, Linear call, identity change or child-ledger mutation was
performed. Historical raw inputs and deployed screenshots remain private.

| Criterion | Fixture evidence | Live evidence / remaining gate |
|---|---|---|
| 1. Lines and roles | API test and browser screenshot show line/role totals, worker sessions, beats, tokens and both prices. | Current deployed dashboard has no Factory tab or accounting JSON endpoint. Configured line coverage remains unverified. |
| 2. Issue cost to ship | Done issue fixture shows both prices, sessions by role and merged PR timestamps. | No gaffer-provided scoped outcome cache; the named historical issue and merge dates are unverified. Historical coverage is shown explicitly. |
| 3. Shipping | Fixture asserts Done/merged/deployed counts and dollars per merge; duplicate identities are counted once. | No cached live outcomes/deploy evidence. No tracker or deployment queries were invented to fill it. |
| 4. Arbitrage and usage | Allocation conserves weekly spend across all roles, survives filters, uses dated rates, and labels token estimates. Measured Codex/Claude adapters have fixtures. | Operator rate/subscription files and same-week Claude usage comparison are absent; API/harness 5% and provider-usage 10-point comparisons are unverified. Shipped TOML files are schemas, not invented prices. |
| 5. Codex and attribution | Cumulative and repeated usage, cached input, reasoning subsets, model changes, exact ledger joins and unknown metadata are tested. | Seven-day local accounting list: **671 Codex sessions; 6 blank-model-or-zero-token rows (zero tokens); 542 worker rows missing an issue**. This criterion is not passed live. |
| 6. Other sessions | Browser/API fixtures include an `other` session in the subscription denominator. | Machine-wide 30-day read completed: **4,756 sessions, 1,283 incomplete** at capture. Historical attribution is incomplete; coverage is not claimed complete. |

## Checks

- `go test ./...` and `go vet ./...` passed in both changed repositories.
- Factory Python accounting fixtures (7 tests) and shell syntax checks passed.
- Kit API, historical pricing, scope, measured usage and Codex regression tests passed.
- Synthetic cross-repository run passed: factory ingestion → exact ledger join → kit pricing → conserved weekly allocation → exact beat lookup.
- Playwright checked the visible factory view and Done issue detail; screenshots contain synthetic data only.
- Existing Go eval/Layer tests passed. The plan's `evals/layer-row.jq` replay command cannot run from the public factory checkout because that file is not present; no live eval replay is claimed.

The deployed endpoint was checked read-only and serves HTML rather than
accounting JSON; its top navigation has no Factory tab. No preview was
created or deployed. The test/build logs and synthetic screenshots are a
stand-in for implementation review only, **not a bypass for the failed live
criterion or proof of a deployed preview**.

## Handoff

The launcher/cache lane can supply exact `session_id` associations via the
content-free `costs/attribution.jsonl` interface documented in the factory PR.
The gaffer supplies scoped, hourly outcomes; an authenticated operator job
supplies the content-free Claude usage observation. Prices/subscriptions are
ordinary operator-editable files. Configure and deploy only after review.

The factory report/beat addition is operator-gated `[contract]` work. New
sessions may have null/pending prices until the hourly pricing snapshot is
available; `cost_usd` is unchanged. Subscription allocations are whole-week
snapshots; a session spanning reset is assigned to its start week. No
historical beat rewrite is performed.
