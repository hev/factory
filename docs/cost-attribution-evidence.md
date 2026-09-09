# Cost-attribution review evidence — 2026-09-08

[Approved cost plan](https://github.com/hev/factory/blob/main/plans/active/factory-inference-cost-attribution.md).
This is an updated implementation handoff. Both companion PRs remain draft.
No merge, deployment, service restart, scheduler activation, Linear call or
child-ledger mutation is part of this handoff. Public screenshots are synthetic;
actual outcomes, accounting, exact-session audits and local screenshots stay
in the worker's private evidence directory.

## Acceptance matrix

| Requirement | Completed evidence | Still unmet |
|---|---|---|
| 1. Per-line/role dashboard | API and browser fixtures cover tokens, beats, workers and both prices. The isolated local branch renders actual machine records and the scoped cache. | The running deployment lacks this view. Complete line/role coverage and both dollar totals cannot be certified while attribution, historical prices and bills are unknown. |
| 2. Issue cost to ship | The supplied completion snapshot, scoped PR merges and verified successful deploy jobs form an actual cache. The specified closed issue renders Done with its exact known sessions and merge records locally. | Earlier sessions/assignment records may be missing; both complete dollar totals and the deployed interaction remain unverified. A partial history is not the whole cost to ship. |
| 3. Shipping | Scoped refresh, schema validation and successful deploy-job verification run. Fixtures test counts and deduplication; local live records render. | Out-of-scope deployment estates are outside this grant. Exact merge-SHA joins do not infer ancestor PRs shipped by later releases. Full historical coverage, dollars per merge and the deployed panel remain unverified. |
| 4. Arbitrage/limits | Official sourced rates, per-request pricing, nullable bills, weekly allocation conservation and reset time are tested. Existing measured Claude/Codex observations match the local display; neither is a token estimate. | Actual billed monthly USD, the precise paid Codex tier, historical effective rates and any additional purchased plans are missing. Full savings are unavailable. Deployed and separate same-week provider-UI comparison remain unverified. |
| 5. Codex and attribution | Exact eval/ledger/harvest/assignment/parent/PR joins run; repeated cumulative usage is deduplicated. No rows are discarded. A private per-session audit records every remaining error and conflict. | Real zero-token sessions, missing worker issues, ambiguous assignments and unproven cached factory identities remain. The strict CLI acceptance fails honestly. Needed input is the exact missing assignment/identity record or a decision about a genuinely zero-usage session; no tokens or issues will be fabricated. |
| 6. Other sessions | Machine-local ingestion includes sessions outside factory roles in the shared subscription denominator. Fixture/browser tests verify `other` survives filtering. | Complete same-subscription coverage cannot be proved from an incomplete local history and unidentified plan purchases. Unknown cached identities remain flagged, not silently accepted as fully attributed. |
| Step 3: API vs harness within 5% | Exact Claude cost-state records were found locally. Current rates are sourced and pricing fixtures pass. | Those exact historical records lack verified applicable dated rates. Eval dollars derived from time-window joins are not exact-session ground truth and are excluded from acceptance. Exact same-period rate/usage coverage is still required. |
| Step 6: beat/report fields | Wrapper-owned nullable fields and exact priced-session lookup pass shell and cross-repo checks. Pending rows become priced on refresh. | New live beats require the reviewed wrapper/contract rollout; no running process was restarted and historical beats were not rewritten. |
| Backfill replay | Go trace/Layer/eval regression tests pass. | The plan's `evals/layer-row.jq` replay command is absent from the public checkout. No live eval-store replay or duplicate-free live backfill is claimed. |

## Runnable checks and privacy

- `go test ./...` and `go vet ./...` passed in both changed repositories.
- All 17 factory Python tests and wrapper/beat shell syntax checks passed.
- Synthetic cross-repo ingestion → exact ledger join → kit pricing → conserved
  weekly allocation → exact beat lookup passed.
- Four Playwright suites passed: factory view, existing dashboard, dashboard
  filter/eval UI, and loading/performance. The independent dashboard worker's
  projection/latency commits are preserved by authorship-retaining cherry-picks.
- The complete refresh command ran against the supplied issue snapshot and
  every configured allowed repository. It creates outcomes, sessions, priced
  rows, beats, attribution audit and completion manifest; no hand-written
  outcomes cache or scheduler activation is needed.
- Existing-grant Claude usage refresh passed without writing credentials.
  An isolated local server rendered actual cached data and measured limits;
  these private screenshots are **not a deployed preview**.

The current deployed evidence is a read-only baseline. The local test/build
logs provide the substantiated no-preview stand-in; no deployed acceptance is
claimed. Private evidence lives under
`~/.factory/evidence/factory/worker-factory-cost-attribution/`; the parent's
handoff there names exact remaining rows and required inputs. The parent keeps
these tails in the existing approved work list. Missing source facts do not
become new issue assignments or implied purchasing decisions.

## Synthetic screenshots

[Lines, shipping, arbitrage and other-role fixture](https://github.com/hev/kit/blob/impl/factory-cost-attribution-0908/docs/images/cost-attribution/criterion-1-3-4-6-fixture.png)

[Done issue cost-to-ship fixture](https://github.com/hev/kit/blob/impl/factory-cost-attribution-0908/docs/images/cost-attribution/criterion-2-fixture.png)
