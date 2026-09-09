# Inference accounting

The factory exports content-free accounting; kit owns prices and presentation.
No identity hooks, network access or provisioning are needed for ingestion.
Standalone ingestion supports Python 3.9+; the complete refresh requires Python 3.11+. Run from a checkout of this repo:

```sh
python3 scripts/costs/ingest.py --days 30 --output ~/.factory/costs/sessions.jsonl
```

This reads the machine's Claude and Codex transcripts, child ledgers, commented
harvest ledgers and cached eval rows. It emits only accounting and attribution,
never prompts, tool output or harvested panes. Paths, session IDs and issue
metadata are still private; do not publish the export. Output files have mode
0600. `beats.jsonl` is exported alongside sessions. Source roots are CLI flags
for fixtures and operators with another directory layout.

The complete refresh is a runnable command for an operator's existing hourly
scheduler. It consumes a parent-provided issue snapshot and ordinary kit price
and subscription files; it makes only repository-scoped GitHub reads:

```sh
python3.11 scripts/costs/refresh.py \
  --config factories/example.toml --issues /private/issues.json \
  --dir ~/.factory/costs --kit-bin /path/to/hev \
  --deploy-workflows /private/deploy-workflows.json \
  --plan-root example=/path/to/approved/plans
```

`refresh.py` locks against overlapping refreshes, builds scoped outcomes first,
then joins exact local sessions, exports the attribution audit, and runs
`hev cost-price`. Every producer must succeed before publication. Each output
file is replaced atomically, and `refresh.json` is written last as the
completion manifest; readers can briefly see mixed files during publication.
No scheduler is installed or enabled. The lookback defaults to 30 days and
must cover every subscription week and historical issue being reviewed.
Missing older records cannot establish an issue's entire cost to ship.

## Scoped outcomes

`outcomes.py` is independently runnable with `--config`, `--issues`, `--output`
and optional `--deploy-workflows`. `--validate-cache FILE --config CONFIG`
validates an existing cache without network access. The normative wire schema
for this adapter is [cost-outcomes.schema.json](../schemas/cost-outcomes.schema.json).
Runtime validation additionally binds `linear_team`, `repo_scope` and unique
issue IDs. There are no Linear or account-global API calls.

The issue input is `{ "team": "example", "refreshed_at": 1788900000000,
"issues": [{"id":"EX-1", "status":"Done", "completedAt":"2026-09-08T00:00:00Z"}] }`.
It contains the parent's already-scoped snapshot. Its original timestamp is
preserved so refreshing GitHub cannot conceal stale issue data. PR associations
require exact known identifiers explicitly referenced in a scoped PR title or
body. Deployment associations require the PR's exact merge SHA and either a
successful production deployment status or an explicitly configured successful
deploy job on a successful push/release/manual workflow run. Ordinary build/test
success and PR CI do not count as deployments. Ancestor PRs of a later release
are not inferred from a different SHA.

The deployment map uses exact verified repository/workflow/job triples:
`{"example/api":[{"path":".github/workflows/deploy.yml","job":"deploy"}]}`.
The operator verifies that the job actually deploys; the adapter does not infer
that from a workflow name. Missing jobs or absent records remain missing.

## Stable interfaces

Kit reads `sessions.jsonl`, keyed by exact harness `session_id`. Each row has
`start`/`end` in Unix milliseconds, `harness`, `model`, `cwd`, `instance`,
`role`, `plan`, `step`, `issue`, `issue_url`, `repo`, and `usage_by_model`.
Usage fields are `input_tokens` (uncached), `cache_read_tokens`,
`cache_creation_tokens` (five-minute), `cache_creation_1h_tokens`, `output_tokens` (including reasoning), and
`reasoning_tokens` (informational subset). Content-free `requests` retain model, timestamp and usage for dated, long-context pricing. No cost rate belongs in this export.
Codex cumulative token events are differenced and repeated totals contribute
nothing. Claude usage is deduplicated by request identity across transcript
files sharing a session. Missing cumulative Codex usage is an explicit error,
not a guessed sum of repeated last-request snapshots.

The launcher/cache worker can append exact associations to
`~/.factory/costs/attribution.jsonl`:

```json
{"session_id":"harness-uuid","instance":"example","role":"worker","repo":"example/api","plan":"search","step":"implement","issue":"EX-1"}
```

This is a separate, content-free accounting sidecar, **not the child ledger**.
It must be populated by the process that knows the harness ID; directory or
session-name similarity is not sufficient. Existing eval `session` IDs join
through `session_name` to the parent-owned live/harvest ledger. The ingester
also accepts an explicit `session_id` on a supplied ledger snapshot, without
writing it. Additional joins use recorded assignment documents linked to their exact tool
call, exact brief paths and dispatch times, harness-issued resume IDs, explicit
subagent parent IDs, wrapper session stamps, exact instance/plan/source mappings
from explicitly supplied `--plan-root INSTANCE=PATH` directories, and ledger PRs with a unique
scoped issue reference. Mere related-issue mentions do not assign a session.
Conflicts remain `issue_candidates`. Cached roles without a proven factory
identity retain their reported role and an error under `other`.

`attribution-pending.json` retains each incomplete session ID, observed usage
kind, assignment sources, candidate issues and errors, without prompts. Missing
worker issues remain errors; zero-token rows remain present. No issue, model or
token amount is invented to satisfy acceptance.

Kit's `hev cost-price --dir ~/.factory/costs --output
~/.factory/costs/priced.jsonl` adds nullable `api_usd` and `sub_usd` from the
operator's kit price/subscription files. The wrapper's exact-session lookup
reads this file via `FACTORY_PRICED_SESSIONS`. A session just finishing may
still be pending until the next refresh. The read side joins on `session_id`;
it must not interpret pending/null as zero or overwrite historical beats.

## Approval and evidence

The one-shot report/beat additions change the runtime contract and require an
operator-approved `[contract]` PR. Pulling files alone does not update a
running gaffer's context; restart remains an operator action. This work neither
restarts a process nor changes the launcher's/reaper's cache lifecycle.

Kit reads the scoped cache and measured provider observations. The optional
`python3 scripts/costs/claude_usage.py --plan PLAN --output
~/.factory/costs/claude-usage.json` consumes an existing GUI-domain keychain grant
and persists only measured weekly usage. It creates no grant and never stores
or prints credentials. A missing grant fails explicitly. Codex usage comes from
existing rollout observations. Missing billed spend remains null even when an
entitlement or measured usage exists.

Run `python3 -m unittest discover -s scripts/tests -p 'test_cost_*.py'`,
`bash -n factory-iterate.sh scripts/factory-beat.sh`, `go test ./...`, and
`go vet ./...`. Fixtures contain only synthetic metadata.
