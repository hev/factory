# Cost-attribution review evidence — 2026-09-09

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
| 3. Shipping | All allowed repositories were refreshed with complete PR pagination, verified deploy jobs, merge-before-landing checks and GitHub ancestry proofs. Synthetic API/browser checks include unassociated deploys without double counting or assigning them to a role. | Out-of-scope deployment estates are outside this grant. Retained GitHub records and explicit issue references bound coverage; absent records/unconfigured jobs do not prove no deployment. Complete historical coverage, dollars per merge, artifact correctness and the deployed panel remain unverified. |
| 4. Arbitrage/limits | Official sourced rates, per-request pricing, nullable bills, weekly allocation conservation and reset time are tested. Existing measured Claude/Codex observations match the local display; neither is a token estimate. | Actual billed monthly USD, the precise paid Codex tier and any additional purchased plans are missing. Unsupported historical rate categories remain unknown. Full savings are unavailable. Deployed and separate same-week provider-UI comparison remain unverified. |
| 5. Codex and attribution | Exact eval/ledger/harvest/assignment/parent/PR joins run; repeated cumulative usage is deduplicated. No rows are discarded. A private per-session audit records every remaining error and conflict. | Real zero-token sessions, missing worker issues, ambiguous assignments and unproven cached factory identities remain. The strict CLI acceptance fails honestly. Needed input is the exact missing assignment/identity record or a decision about a genuinely zero-usage session; no tokens or issues will be fabricated. |
| 6. Other sessions | Machine-local ingestion includes sessions outside factory roles in the shared subscription denominator. Fixture/browser tests verify `other` survives filtering. | Complete same-subscription coverage cannot be proved from an incomplete local history and unidentified plan purchases. Unknown cached identities remain flagged, not silently accepted as fully attributed. |
| Step 3: API vs harness within 5% | Dated provider sources establish Sonnet/Haiku base and five-minute cache rates, plus Sonnet/Opus one-hour rates from August 23 and Fable 5.1 launch rates from September 1. All 31 exact cost-state prefixes are priced: 1 reconciles full model counters and matches within 5%; 6 other close dollar matches are rejected for incomplete usage. | 30 records still lack reconciled complete model/auxiliary usage; the whole-population 5% gate remains unmet. Earlier one-hour categories and complete historical OpenAI tables remain unsupported as detailed in the rate-source record. Time-window eval dollars are excluded; harness estimates are not bills. |
| Step 6: beat/report fields | Wrapper-owned nullable fields and exact priced-session lookup pass shell and cross-repo checks. Pending rows become priced on refresh. | New live beats require the reviewed wrapper/contract rollout; no running process was restarted and historical beats were not rewritten. |
| Backfill replay | The public replacement is factory `eval_rows.py` → kit `hev eval put`. Structured findings are preserved as canonical JSON strings. A frozen 1,423-row private input replayed twice into the disposable HTTP contract store: 1,423 distinct IDs and no changed rows. Synthetic batching/UTC/new-grade/invalid-input checks pass. | Real hosted-store/embedding behavior and production backfill remain rollout gates. The isolated test is not a production replay. |

## Runnable checks and privacy

- `go test ./...` and `go vet ./...` passed in both changed repositories.
- All 23 factory Python tests and wrapper/beat shell syntax checks passed.
- Previously verified synthetic cross-repo ingestion → exact ledger join → kit pricing → conserved
  weekly allocation → exact beat lookup passed.
- Four Playwright suites passed: factory view, existing dashboard, dashboard
  filter/eval UI, and loading/performance. The independent dashboard worker's
  projection/latency commits are preserved by authorship-retaining cherry-picks.
- The previous complete refresh command ran against the supplied issue snapshot and
  every configured allowed repository. It creates outcomes, sessions, priced
  rows, beats, attribution audit and completion manifest; no hand-written
  outcomes cache or scheduler activation is needed.
- Previous existing-grant Claude usage refresh passed without writing credentials.
  An isolated local server rendered actual cached data and measured limits;
  these private screenshots are **not a deployed preview**.

The current deployed evidence is a read-only baseline. The local test/build
logs provide the substantiated no-preview stand-in; no deployed acceptance is
claimed. Private evidence lives under
`~/.factory/evidence/factory/worker-factory-cost-source-completion/`; the
previous attribution audit remains under `worker-factory-cost-attribution/`.
The new handoff records exact comparison gaps, input hashes, cache timestamps,
ancestry results and every unassociated deployment. The issue snapshot was
not fetched again or made fresh by the GitHub refresh. The parent keeps
these tails in the existing approved work list. Missing source facts do not
become new issue assignments or implied purchasing decisions.

## Source completion checks

- [Dated rates and unsupported history](https://github.com/hev/kit/blob/impl/factory-cost-attribution-0908/docs/cost-rate-sources.md): dated-rate regression checks cover launch boundaries, cancelled September price increase and one-hour availability.
- `scripts/test-cost-source.py` checks exact cost-state boundaries, repeated request deduplication, recorded subagent session joins and rejection of missing auxiliary usage. The private 31-record comparison exits 1 intentionally: only one record passes all conditions.
- `scripts/costs/eval_rows.py` handles historical structured findings; direct raw replay was tested and failed, which led to this adapter. `scripts/test-eval-replay.py --input ADAPTED_FILE` proves duplicate-free replay of the frozen private input against the isolated contract store. No real evaluation prose is committed.
- Scoped refresh scanned 125 PRs across seven allowed repositories; the supplied snapshot has 23 issues and 27 PR/issue links. It found 14 verified deployments: 3 have eligible issue ancestry and 11 remain unassociated. The cache retains all 14 for unfiltered/project shipping counts and preserves the original issue snapshot timestamp. These source counts are not a completeness claim about another host or estate.
- No visual layout changed. The factory browser fixture checks the shipping count and Done issue interaction; existing dashboard, filter/eval and loading/performance suites remain regression checks. Synthetic screenshots from this run remain private and are not deployed previews.

Actual bills/tier/inventory and legacy completeness decisions remain with
[FAC-18](https://linear.app/hevmind/issue/FAC-18). No assignment search was
repeated and no new decision was invented. Factory's combined contract,
preview and shared-cache gates remain in its
[combined handoff](https://github.com/hev/factory/blob/impl/factory-cost-attribution-0908/docs/combined-contract-handoff.md),
including the unanswered [FAC-24](https://linear.app/hevmind/issue/FAC-24).

## Synthetic screenshots

[Lines, shipping, arbitrage and other-role fixture](https://github.com/hev/kit/blob/impl/factory-cost-attribution-0908/docs/images/cost-attribution/criterion-1-3-4-6-fixture.png)

[Done issue cost-to-ship fixture](https://github.com/hev/kit/blob/impl/factory-cost-attribution-0908/docs/images/cost-attribution/criterion-2-fixture.png)
