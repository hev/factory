# kit dashboard: one row per trace, search and filters run in Layer, marks on every trace

> **ARCHIVED 2026-09-07** — all 8 steps shipped. Steps 1–6 and 8 in
> [hev/kit#25](https://github.com/hev/kit/pull/25); step 7 (grader writes to
> Layer) in [hev/factory-pro#44](https://github.com/hev/factory-pro/pull/44),
> merged 2026-09-07T21:51Z. Success measure ("find the session that did a
> thing and see whether it went well without reading transcripts") reads as
> met on the merged diff — search, filters and marks all run in Layer, one
> row per session; the second-pass RFC ([FAC-11](https://linear.app/hevmind/issue/FAC-11/kit-dashboard-loads-in-a-second-and-says-so-filters-without-a-search),
> [plan](../active/kit-dashboard-fast-load-and-preview.md)) exists because the
> operator used it and found the load-time and preview gaps that follow.
> One tail: FAC-9 step 1's one-time `hev index --read-side --force` backfill
> is carried forward as FAC-11 step 9 rather than repeated here.

> Source: https://linear.app/hevmind/issue/FAC-9/kit-dashboard-one-row-per-trace-search-and-filters-run-in-layer-marks

As the operator of two machines' worth of agent traces, I want the kit dashboard to search the archive in Layer and filter it the way I think about it (these projects, these models, this tool, over this cost, and how the session was graded), showing each trace once, so that I can find the session that did a thing and see whether it went well without reading transcripts.

## What is wrong today (verified on the mini, 2026-09-07)

* Search never leaves the browser. The page filters summary, project and first prompt by substring (`template.html` line 322) and there is no `/api/search`. `hev find` has the hybrid search; the page does not.
* One filter, single-valued: `window` server-side; `filterProject` and `filterModel` are single chips set from the stats table.
* The same trace appears many times: 1739 rows for 1300 sessions over 7d, one id 26 times. Session row ids are a content hash, so every index pass over a growing session writes a new row and the old ones stay. These are stale session rows, not chunks.
* "When you prompt" is blank: `corpus()` emits `prompt_ts: []` unconditionally; the session row carries no prompt timestamps.
* "Tokens per day by model" and "Spend per day" have tooltips and no click handler.
* Marks exist (279 rows in `~/.factory/evals/evals.jsonl` on the mini: four marks, poor, evidence, findings per session) and nothing reads them.
* The wordmark reads `hev kit_`.

## Acceptance criteria

* `/api/sessions?window=7d` returns one row per session id, and the list shows each trace once, at its latest state.
* Typing a query and pressing Enter returns traces ranked by Layer's hybrid search over their chunks, each with its matching snippet; clicking the snippet opens the trace at that turn. No request is made while typing.
* Filters compose with search and with each other: several projects, several models, harness, host, one or more tool names, and ranges on tool count, tokens, cost and wall-clock minutes. Every filter runs in the store, and the URL carries the filter state.
* The heatmap has data for any window with prompts. Clicking a day column, a model segment or a heatmap cell opens the trace list filtered to it.
* Every graded trace shows its four marks in the list and its evidence and findings in the trace view; the list can be filtered to poor, by a mark's ceiling, by role and by instance, and a search matches evaluator prose as readily as transcript text.
* The wordmark reads `hev_ kit`, cursor between the words.

## How to test it

1. Open http://100.126.12.95:8787 from the laptop after the merge deploys (kit CI's "deploy to the mini" job restarts `com.hev.serve`).
2. `curl -s 'http://100.126.12.95:8787/api/sessions?window=7d' | jq '[.sessions[].id] | length, (unique|length)'` prints the same number twice.
3. Search `preflight` and press Enter. Filter to projects `lyr` + `layer-pro` and model `gpt-6-astra`: the count drops and every row matches. Add tool `Bash` and cost >= $1.
4. Stats: the heatmap has filled cells. Click a "Spend per day" column and land on that day's traces.
5. Filter to poor only: every row shows a poor pill. Open one: the Eval panel shows four marks with evidence turns, and the findings. Search a phrase from that evidence and press Enter: the same trace comes back with the evaluator's line as its snippet.

## The work

1. **One row per session.** The session row `id` becomes the session id so a re-index upserts; the server also folds duplicates by `session_id`, keeping the latest `end`, for namespaces written before this. Run `hev index --read-side --force` once on both hosts. *Accept:* test 2.
2. **The session row carries** `prompt_ts []uint`**,** `tool_names []string` **(distinct tool names used) and** `total_tokens int`, written by `trace.Sessions`, declared in `sessionSchema`, passed through by `corpus()`. *Accept:* the heatmap draws; after the backfill `jq '[.sessions[]|select(.prompts>0 and (.prompt_ts|length)==0)]|length'` is 0.
3. **Filters in the store.** `/api/sessions` and `/api/stats` accept `project`, `model`, `harness`, `host`, `tool` (each repeatable), `tools_min/max`, `tokens_min/max`, `cost_min/max`, `wall_min/max` (minutes) and `since/until`, composed into one `And` filter: `In` for strings, `ContainsAny` for tools, `Gte`/`Lte` for ranges. Page: a filter bar with multi-select chips for project, model and tool, range inputs for the rest, state in the query string; stats and charts obey the same filters. *Accept:* test 3, and each count equals a `jq` over the unfiltered corpus.
4. **Search in Layer.** `/api/search?q=&top=50` plus the step 3 filters: resolve the filtered session ids from the sessions namespace, run `Client.Search` on chunks with `session_id In [ids]`, group hits by session, return sessions ranked by best hit with snippet, turn uuid and role. Page: the box submits on Enter or the button and the results replace the list with a snippet column; the in-trace substring finder stays for the loaded trace. *Accept:* `curl '/api/search?q=preflight&project=lyr'` returns only lyr sessions, each with a snippet; clicking a hit lands on its turn.
5. **Charts click through.** A day column sets `since`/`until` to that day; a model segment adds `model`; a heatmap cell filters the loaded list by weekday and hour over `prompt_ts`, the one client-side filter. *Accept:* test 4, and the list count equals the tooltip's count.
6. **Marks in the store.** New `<ns>-evals` namespace, declared by kit: `text` (summary, evidence and findings joined, embedded and full-text indexed like a chunk), `session_id`, `ts`, `role`, `instance`, `host`, `poor`, one `mark_<name>` int column per mark, and the raw `marks`, `evidence` and `findings` as unfilterable JSON strings. `hev eval put` reads JSON rows `{session, ts, role, instance, host, marks{name:int}, poor, summary, evidence{name:text}, findings[]}` on stdin or from a file, flattens the marks, and writes with id = hash(session, ts), so a re-put is a no-op. `/api/sessions` joins the newest eval per session into `eval`; filters `poor`, `mark_<name>_max`, `role`, `instance`. `/api/search` queries the evals namespace beside the chunks, so a query matches evaluator prose and returns the trace with that snippet. List: a marks column (`4·5·5·5`) and a poor pill. Trace view: an Eval panel with evidence per mark and the findings. kit hard-codes no mark names; they come from the rows. *Accept:* test 5; `docs/rfcs/0005-marks.md` documents the row shape.
7. **The grader writes to Layer.** In hev/factory-pro, `evals/factory-eval` pipes each graded row to `hev eval put` at grade time, beside the local jsonl the foreman still reads; `hev eval put < ~/.factory/evals/evals.jsonl` once backfills the 279 rows already there. *Accept:* a session graded by the last timer fire shows its marks on the page; a search for a phrase from its evidence returns it.
8. **Wordmark.** `hev<i>_</i> kit` and the matching `<title>`. *Accept:* screenshot in the PR.

## Constraints

* Layer is the only source. Nothing filters in the browser except the loaded trace's finder and the heatmap cell in step 5.
* No JS build; the page stays one vanilla template.
* kit stays factory-agnostic: no instance, role or mark names are hard-coded; the eval row is a documented generic shape.
* `hev find` is unchanged; `/api/search` reuses `Client.Search`.
* If the `session_id In [...]` list exceeds the store's filter limit, put `model` and `repo_url` on chunk rows at index time and filter there; say so in the PR.
* A query returns at most 10,000 rows (`top_k` cap). After step 1 an unfiltered `all` or `90d` list can exceed it; page, or aggregate in the store, before widening the window rather than truncating silently.

## Out of scope

Auth; playback; Codex transcript parity; the rates table; grading itself (FAC-1, https://linear.app/hevmind/issue/FAC-1/every-session-is-graded-and-the-poor-ones-become-one-suggestion-on-the).
