# kit dashboard: loads in a second and says so, filters without a search, previews a trace on hover, draws a grade against the averages

> Source: https://linear.app/hevmind/issue/FAC-11/kit-dashboard-loads-in-a-second-and-says-so-filters-without-a-search

As the operator reading my agents' traces from a phone or a laptop, I want the kit dashboard to load fast and say when it is loading, take my filters without a search, show me a trace before I open it, and draw a grade against the averages, so that I can judge a session in seconds instead of scrolling a table and reading a wall of evaluator prose.

Builds on <issue id="03739a32-96fb-4326-85b2-0ba1e4ebc4d2" href="https://linear.app/hevmind/issue/FAC-9/kit-dashboard-one-row-per-trace-search-and-filters-run-in-layer-marks">FAC-9</issue> ([https://linear.app/hevmind/issue/FAC-9](<https://linear.app/hevmind/issue/FAC-9>)), which put search, filters and marks in Layer. This is the second pass: the operator used it for an afternoon and this is what got in the way. Measured on the laptop against the same namespace the mini serves, 2026-09-07, 30d window.

## What is wrong today

* First paint waits on Layer. `/` is 39 MB and 6.6 s; `/api/sessions` is 38 MB and 3.3 s; `/api/stats` returns 232 bytes in 3.1 s. 96% of the list payload (36.3 MB) is `first_prompt`, shipped in full for every row; the other fields total 1.6 MB.
* The loading state is one line of small muted text ("Loading traces…") above the filter bar. While it shows, the old table stays on screen and nothing else changes, so a filter click looks ignored.
* "When you prompt" is blank. The heatmap draws `prompt_ts`, which the read side writes at index time; 114 of 3,033 sessions in the window have it because the one-time `hev index --read-side --force` from <issue id="03739a32-96fb-4326-85b2-0ba1e4ebc4d2" href="https://linear.app/hevmind/issue/FAC-9/kit-dashboard-one-row-per-trace-search-and-filters-run-in-layer-marks">FAC-9</issue> step 1 has not run on either host. The page shows an empty grid instead of saying so.
* Eight range boxes (tools, tokens, cost, wall minutes, min and max) and four typed mark ceilings sit between the window control and the list. The operator does not use the ranges; the ceilings are 1–5 integers typed blind.
* `since`/`until` are two free-text boxes in the filter bar, apart from the 7d/30d/90d/all control they belong with.
* The search box sits below the filters and reads as required; the count line appears only after a search or a range submit.
* Nothing previews a trace: a row is an id, a repo and numbers.
* The Eval panel is prose: the four marks are `contract · 5` headings over paragraphs, with nothing to compare a 3 against.
* No one can see how long Layer took. The server measures nothing and returns no timing.

## Acceptance criteria

* Opening `/` paints the chrome and a visible loading state within 500 ms; the list for a 30d window appears within 1.5 s on the mini. `/api/sessions?window=30d` transfers under 2 MB with negotiated gzip (`curl --compressed`); plain JSON remains lossless and is not subject to that transfer bound.
* Every reload (window, filter, search) shows one unmistakable loading state: the count line reads "Loading…", the table dims, and a progress bar runs under the top bar until the response lands. A failed load says what failed in the same place.
* The count line ends with Layer's own time: "3,033 traces · $17,112 · Layer 1.2 s, 2 queries". Stats shows the same for its queries.
* Picking any facet reloads the list at once with no search text and no button press. The search box sits above the facets and is optional.
* The filter bar has the seven facets, the grade dropdown, and one dropdown per mark ("contract: any / ≤ 2 / ≤ 3 / ≤ 4"). The tools, tokens, cost and wall range boxes are gone from the page (the API keeps them). The mark names still come from the rows.
* The window control reads `7d 30d 90d all custom`; custom opens two date pickers and shows the chosen range in its place.
* The heatmap fills for any window with prompts once the reindex has run; until then it reads "114 of 3,033 traces carry prompt times, run `hev index --read-side --force`" over the empty grid.
* Hovering a trace row for 300 ms shows a card: the first prompt (first 600 characters), the summary, the tool mix as a bar, the four marks if graded. Leaving hides it; no request is made.
* The Eval panel draws each mark as a horizontal 1–5 bar with two tick marks, the archive average and this project's average, each labelled with its n; evidence sits under its bar; poor is a pill on the header.

## How to test it

1. Open [http://100.126.12.95:8787](<http://100.126.12.95:8787>) on the mini after CI deploys. The operator's laptop-origin walkthrough is optional and is not a merge gate. Watch the first second: chrome, progress bar, then rows. `curl --compressed -s -o /dev/null -w '%{size_download} %{time_total}' 'http://100.126.12.95:8787/api/sessions?window=30d'` prints under 2000000 and under 1.5.
2. Click `+ project` and pick `lyr`: the bar runs, the table dims, the count line changes, no search typed. The count line ends in a Layer time.
3. Click `custom`, pick Sep 1 to Sep 3: the list and stats narrow, the URL carries `since`/`until`.
4. Set `outcome: ≤ 3`: every row's outcome mark is 3 or less.
5. Hover a row: the card shows the prompt and marks. Move off: it goes.
6. Open a graded lyr trace, Eval tab: four bars with two ticks each, and the tick labels give the archive n and the lyr n.
7. Stats: the heatmap either has cells or names the coverage and the command.

## The work

1. **Slim list rows.** The session row gains `first_prompt_short` (first 600 chars) at index time; `/api/sessions` and `/api/search` return it instead of `first_prompt` and drop `prompt_ts` and `tool_names` from list rows. `/api/session/{id}` keeps everything. The page stops inlining the corpus into `/`; it renders the chrome and fetches. *Accept:* test 1's numbers, and the hover card in test 5 needs no request.
2. **Heatmap and coverage from the server.** `/api/stats` returns a 7×24 `prompt_grid` and `prompt_coverage {with, total}` computed from `prompt_ts` server-side; the page draws from the grid and shows the coverage line when `with < total`. A heatmap cell click sends `weekday`/`hour` to `/api/sessions`, which applies them server-side over the `prompt_ts` it already fetched (a weekday is not a store filter), so the page holds no `prompt_ts`. *Accept:* test 7; `jq .prompt_coverage` on the stats endpoint matches `jq '[.sessions[]|select(.prompt_ts|length>0)]|length'` against the detail rows.
3. **Layer timing.** Every Layer call in `internal/layer` is timed; each API response carries `timing {layer_ms, queries, rows}` and a `Server-Timing: layer;dur=` header; the count line and the stats header show it. *Accept:* test 2's count line; `curl -sI` shows the header.
4. **Loading state.** One `setLoading(on, err)` drives a 2px progress bar under `.top`, the "Loading…" count line, and `opacity:.4` on the table; every fetch path calls it; an error replaces "Loading…" with the message. *Accept:* test 2; a fetch against a stopped server shows the error where the count was.
5. **Filter bar.** Search box above the facets; facets reload on change; the eight range inputs and the "Apply ranges" button are removed; the four mark ceilings become dropdowns built from the mark names in the rows with options any/≤2/≤3/≤4; the grade dropdown stays. *Accept:* tests 2 and 4; a screenshot of the bar in the PR.
6. **Custom window.** `custom` joins the window buttons; it toggles two `<input type=date>` fields, writes `since`/`until`, and shows "Sep 1 – Sep 3" as the active button label; picking 7d/30d/90d/all clears them. *Accept:* test 3.
7. **Hover card.** A 300 ms hover on a list row shows a fixed-position card built from the slim row: prompt, summary, a stacked tool-mix bar from `tool_counts` (add `tool_counts {name:int}` to the slim row, at most the top 6), and marks. Keyboard focus shows it too. *Accept:* test 5; screenshot in the PR.
8. **Eval bars.** `/api/session/{id}` returns `eval_baseline {overall {mark: avg, n}, project {mark: avg, n}}` computed from the newest eval per session in the namespace, cached 60 s. The Eval panel draws one bar per mark: filled to the mark, ticks at the two averages, labels `all n=338 · lyr n=41`; evidence under each; findings last. *Accept:* test 6; screenshot in the PR.
9. **Reindex (mini only).** Run `hev index --read-side --force` on the mini. The laptop half is struck as out of factory scope under [https://linear.app/hevmind/issue/FAC-20](<https://linear.app/hevmind/issue/FAC-20>); the operator's laptop backfill and laptop-origin walkthrough are not merge gates. *Accept:* coverage on the stats endpoint reads `with == total` for 30d sessions whose host is the mini. The authorized mini replay completed 3,438/3,438 units with zero errors; evidence: [https://github.com/hev/kit/pull/30](<https://github.com/hev/kit/pull/30>).

## Constraints

* Layer stays the only source and the page filters nothing itself. Step 2 moves the one client-side filter <issue id="03739a32-96fb-4326-85b2-0ba1e4ebc4d2" href="https://linear.app/hevmind/issue/FAC-9/kit-dashboard-one-row-per-trace-search-and-filters-run-in-layer-marks">FAC-9</issue> allowed into the server.
* No JS build, no chart library; the page stays one vanilla template. Date pickers are native inputs.
* kit hard-codes no mark names, roles or instances; dropdowns and bars are built from the rows.
* Range filters stay in the API for `hev find` and for the URL; only the inputs go.
* Nothing here changes the write side except `first_prompt_short` and `tool_counts` on the session row, both filled by the same reindex.

## Out of scope

Auth; a JS framework; new charts on Stats; mobile layout; changing what the grader writes (<issue id="4520710c-6971-4177-a073-8be23c001f7f" href="https://linear.app/hevmind/issue/FAC-1/every-session-is-graded-and-the-poor-ones-become-one-suggestion-on-the">FAC-1</issue>).

