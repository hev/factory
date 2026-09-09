# Cost source acceptance — 2026-09-09

[Approved plan, including operator amendment 7548db0](https://github.com/hev/factory/blob/7548db0/plans/active/factory-inference-cost-attribution.md).
Paired source: [factory PR18](https://github.com/hev/factory/pull/18) and
[kit PR32](https://github.com/hev/kit/pull/32). Source acceptance passes;
operator merge and separately authorized rollout still gate deployed acceptance.
No deployed cost view or new live beat/report field is claimed.

## Amended acceptance matrix

| Plan requirement | Source acceptance and exact evidence | Remaining acceptance after operator merge |
|---|---|---|
| Step 1 / criterion 5: Codex rows, 30-day ingestion | Real local refresh ingested 5,606 available sessions. The seven-day view retains all 5,606, including 1,724 incomplete model/usage/attribution rows. Source counters and models survive ingestion; repeated cumulative events are deduplicated. Missing facts remain missing. | Production ingestion/backfill on the serving host and verification of source-backed rows there. Local history starts September 3; a 30-day command cannot create older unavailable files. |
| Step 2 / criteria 5–6: attribution and other | Exact ledger/eval/harvest/assignment/plan/parent/PR joins remain intact. No exhausted legacy search repeated. Non-factory sessions remain `other` in the allocation denominator; ambiguous assignments remain flagged. Browser verifies one coverage line inside Arbitrage for both its billing week and the selected window. | Verify serving-host coverage with the same incomplete-row policy. Unknown historical identity, tokens and issues are accepted gaps, not requests for a new decision. All-host subscription completeness is not established by this machine. |
| Step 3: two prices and 5% comparison | Dated rate boundaries and exact-session cross-source regression pass. Reused frozen audit: 31 exact Claude records priced, 1 comparable complete record passes 5%; 30 have incomplete counters. Six coincidentally close dollar matches remain excluded. No whole-population completeness gate is imposed. | Keep genuine unsupported historical model/date/cache/service-rate categories unknown. Apply verified new rates when evidence exists; do not backdate current rates. These boundaries remain in kit's rate-source document. |
| Step 4 / criteria 2–3: outcomes | Fresh parent-provided factory-team snapshot: 25 issues, timestamp 1788925332851 preserved; GitHub refresh 1788925645195. All pages across seven configured repositories retain 125 PRs and 15 verified deployments. Seven-day shipping: 4 Done issues, 51 merged PRs, 12 deployments. Unreferenced PRs/deploys count globally without invented issue or role joins. Exact ancestry and successful configured deploy jobs bound associations. | Arrange hourly scoped issue snapshots and refresh after rollout. Retained records, explicit references and configured deploy jobs bound historical coverage. Ancestry proves commit inclusion, not artifact contents, absence of reverts or complete historical shipping. |
| Step 5 / criterion 1: per-line and role screen | Fixture API/browser and isolated real-data browser pass: beats, workers, sessions, tokens, both price columns and role splits. | Independently exercise deployed factory view, filters/window and both prices; capture private screenshots for the parent to attach to the existing issue. |
| Step 5 / criterion 2: closed issue | Local FAC-9 interaction renders Done, recorded sessions by role, exact merge timestamps and cost-to-ship availability. Prior scoped merge verification is reused. Partial history is labelled; unknown bills do not produce a false ship cost. | Independently verify the deployed closed-issue interaction and merge dates. Full historical cost-to-ship is not certified from incomplete sources. |
| Step 5 / criterion 3: shipping screen | Fixture and real local rendering pass, including unassociated records, deduplication and attribution filters. Dollars per merge remain unavailable when API totals are incomplete. | Independently verify deployed shipping and its retained-record boundaries. |
| Step 5 / criterion 4: arbitrage and subscriptions | `subscriptions.toml` drives allocation. Missing monthly prices and Codex `tier` render labelled operator-editable placeholders. Savings stays unavailable. Local browser edits synthetic prices/tier, reloads without restart and observes allocation, then restores placeholders. Fixed weekly allocation/conservation tests pass. Fresh existing-grant Claude usage and recorded Codex weekly usage render as measured observations. | Operator fills actual figures/tier when available; this is not a source blocker. Separate same-week Claude provider-UI comparison within 10 points and deployed arbitrage verification remain. Unknown bills are never zero and synthetic editability figures are never billing evidence. |
| Step 6: beat/report | Preserved combined contract, nullable wrapper fields and exact session-price lookup. Cross-repo ingestion → pricing → conserved allocation → beat lookup evidence reused; shell syntax rechecked. | After merge and authorized rollout/restart, verify new beat JSONL and report fields, including pending/null states. Never rewrite historical beats. |
| Supported eval replay | Factory `scripts/costs/eval_rows.py` replaces the absent `evals/layer-row.jq`; it preserves structured findings as canonical JSON strings. Current kit binary replays frozen 1,423-row private adapted input twice through `hev eval put`: 1,423 distinct IDs, unchanged rows. Synthetic batching, equivalent UTC timestamps, new grade and invalid-input checks pass. | Hosted-store/embedding semantics and production backfill remain runtime gates. Disposable loopback HTTP contract-store acceptance is not production replay. |

## Checks and evidence boundaries

- `go test ./...` and `go vet ./...` pass in both repositories.
- All 24 factory Python tests, shell syntax and `scripts/test-cost-source.py` pass.
- Four existing bundled-Chromium browser suites pass on isolated ports: factory,
  dashboard, filter/eval UI and performance/loading. Fixture paint: chrome 36 ms,
  rows 125.5 ms; this is not serving-host latency acceptance.
- Real local refresh, pricing, coverage, Done issue, measured-limit rendering and
  editable-placeholder browser acceptance pass. Original placeholder config was
  restored after synthetic editability checks. Real screenshots stay local.
- Both full diffs were self-reviewed. Main was integrated without force pushes;
  kit's original PR31 cherry-picks remain preserved and PR31 is merged/deployed.
  Its independent deployed-dashboard worker owns fresh dashboard fixes.

Private current logs, local snapshots, browser checks and screenshots:
`~/.factory/evidence/factory/worker-factory-cost-resume-0909/`.
Reused immutable comparison, rate and replay-input evidence:
`worker-factory-cost-source-completion/`; prior exact joins and integration:
`worker-factory-cost-attribution/`. Public fixture images are synthetic.
The complete comparison command's strict all-record exit 1 remains diagnostic;
it is not an amended source acceptance failure or permission to fabricate usage.

No Linear calls, sibling-team reads, account-global GitHub searches, production
writes, merges, deployments, service restarts, scheduler activation or credential
changes. The parent handoff retains every runtime tail above. Factory's preserved
preview/cache policies and their separate live tails are recorded in its
[combined handoff](https://github.com/hev/factory/blob/impl/factory-cost-attribution-0908/docs/combined-contract-handoff.md).
