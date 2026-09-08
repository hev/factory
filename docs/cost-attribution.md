# Inference accounting

The factory exports content-free accounting; kit owns prices and presentation.
No identity hooks, network access or provisioning are needed for ingestion.
Python 3 is required. Run from a checkout of this repo:

```sh
python3 scripts/costs/ingest.py --days 30 --output ~/.factory/costs/sessions.jsonl
```

This reads the machine's Claude and Codex transcripts, child ledgers, commented
harvest ledgers and cached eval rows. It emits only accounting and attribution,
never prompts, tool output or harvested panes. Paths, session IDs and issue
metadata are still private; do not publish the export. Output files have mode
0600. `beats.jsonl` is exported alongside sessions. Source roots are CLI flags
for fixtures and operators with another directory layout.

The job can be run hourly by an operator's existing scheduler. There is no
installer, host mutation or scheduler enabled by this PR. A refresh replaces
one snapshot atomically. The lookback must cover every week being allocated;
use more than 30 days when reviewing older issues. The dashboard states the
start of recorded history; a 30-day backfill cannot establish an older issue's
entire cost to ship.

## Stable interfaces

Kit reads `sessions.jsonl`, keyed by exact harness `session_id`. Each row has
`start`/`end` in Unix milliseconds, `harness`, `model`, `cwd`, `instance`,
`role`, `plan`, `step`, `issue`, `issue_url`, `repo`, and `usage_by_model`.
Usage fields are `input_tokens` (uncached), `cache_read_tokens`,
`cache_creation_tokens`, `output_tokens` (including reasoning), and
`reasoning_tokens` (informational subset). No cost rate belongs in this export.
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
writing it. Missing worker issues remain `attribution_errors`; no issue is
invented and workers never call Linear. Unattributed sessions are `other`.

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

The outcome adapter in kit only reads a gaffer-provided cache with explicit
team and repository scope. Missing cache data, subscription configuration,
provider usage observations and historical attribution are acceptance gaps,
not permissions to query a tracker or invent accounting.

Run `python3 -m unittest discover -s scripts/tests -p test_cost_ingest.py`,
`bash -n factory-iterate.sh scripts/factory-beat.sh`, `go test ./...`, and
`go vet ./...`. Fixtures contain only synthetic metadata.
