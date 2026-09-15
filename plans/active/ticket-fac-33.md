> Approved source: https://linear.app/hevmind/issue/FAC-33/the-floor-digest-wakes-gaffers-on-file-mtime-so-a-waiting-ci-record

## Problem

`scripts/factory-controller.py` `snapshot()` builds the floor digest from `[st_mtime_ns, st_size]` of each watched file. `poll()` runs `factory ci poll <instance>` on every poll whenever a CI record exists, and that atomically rewrites the record — a new mtime even when nothing about the CI state changed. The digest moves, `event(session, 'floor:' + rev, ...)` fires, and a `codex exec` model turn runs. Every 300s, for as long as a PR sits waiting on CI.

The gate itself is fine: `run_turn` returns without a model call when no events are pending. The event source is what is wrong — the controller wakes the gaffer because the controller touched a file the gaffer watches.

Three related scope problems in the same function:

* `ci/<instance>/*.json` and `children/worker-<instance>-*.json` are globbed instance-wide, so every gaffer of an instance wakes on any one gaffer's CI record.
* `values['workers']` is a machine-wide `tmux list-sessions`, so any worker anywhere moves every gaffer's digest.
* `events/<instance>.jsonl` is written by the gaffer's own turn, which is self-feeding.

## Evidence

Measured on adams-mac-mini from `~/.factory/controller/runs/*/*/prompt.txt`, 24h to 2026-09-14T17:40Z — **525 turns**:

| events in the payload | turns |
| -- | -- |
| `floor-change` alone | 361 |
| `floor-change` + `resync` | 136 |
| anything with real content (approved, linear-update, steering, assignment-report, message) | \~15 |

By assignment: `gaffer-factory-ticket-fac-27` 164, `gaffer-factory-ticket-fac-28` 158, `gaffer-lyr-hev-06-docs-editions-plan` 96, `gaffer-lyr-ticket-lyr-29` 92, foreman 13, <issue id="d71d5bae-fbed-487f-8e91-7f1e6f7975ce" href="https://linear.app/hevmind/issue/LYR-51/rfc-0116-six-demos-on-ce-pgvector-with-compose-pipelines-and-cpu">LYR-51</issue> and <issue id="14bd45c6-d415-4f44-ba85-6e9693ef712f" href="https://linear.app/hevmind/issue/LYR-52/06-replace-integer-hevlayer-upserted-at-with-a-date-field-retire">LYR-52</issue> one each. 94% of turns fired on nothing having happened.

The driver: `~/.factory/ci/factory/67d6b79d2b76cfa0415f22288649b4b3.json` is `state=waiting` on hev/factory#29 with a 24h deadline, and its mtime tracks the poll. <issue id="43397ea6-7f08-434f-bf47-a327d14422aa" href="https://linear.app/hevmind/issue/FAC-27/loop-schedules-are-configured-in-a-browser-create-edit-pause-and-run">FAC-27</issue> and <issue id="b577dcd7-bf12-4c03-abf5-eb0319943195" href="https://linear.app/hevmind/issue/FAC-28/an-approved-plan-runs-for-the-cost-of-the-work-not-the-cost-of">FAC-28</issue> are both in instance `factory`, which is why they are the 164/158 pair.

Cost: 246 factory-cwd codex turns in the same window came to 70.4M input (59.8M cache read) and 0.51M output. At the per-turn ratio of a sampled turn ($1.23) that is roughly $300/day API-equivalent, nearly all of it spent re-reading an unchanged floor. `gaffer-factory-ticket-fac-27` ran 164 turns while its report file went unchanged from 07:02Z to 17:29Z.

## Done when

* `snapshot()` digests semantic content, not stat: CI on its state fields (state, pr, head, conclusion), worker ledgers on status. Rewriting a record with identical meaning produces no event — demonstrated by a test that writes the same content twice and asserts the digest is unchanged.
* The worker/tmux inputs are scoped to this assignment's instance (`worker-<instance>-*`), not the machine.
* `events/<instance>.jsonl` no longer enters the digest by stat; a gaffer's own turn cannot wake itself.
* Measured over 24h after the change on the same floor, `floor-change`-only turns for a gaffer with a PR in `state=waiting` drop to single digits per day, counted the same way as the evidence above.
* `contracts/event-controller.md` states what counts as a floor change, so the behaviour is in the contract and not only in the code.

Filed by reception at the operator's request, 2026-09-14.
