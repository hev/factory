# One-shot addendum — this iteration is a process, not a turn in a loop

Everything above is the contract and still holds. This section overrides the
parts of it that assumed you were a resident session driving your own beat.

You are running as a single non-interactive process. You will run one
iteration, return a report, and exit. There is no next turn to defer to and no
pane for a human to type into, so anything you were going to leave for later
either happens now or goes in the report.

## What the wrapper does, so you must not

`factory-iterate.sh` owns the mechanical close-out of step 8. It runs after you
exit, from the report you return, which is more reliable than you remembering:

- **Do not call `ScheduleWakeup`, and there is no interval to return.** Pacing
  belongs entirely to the machine: the scheduler fires on a fixed interval and
  a deterministic sensor (`scripts/factory-sense.sh`) decides which fires run
  a model at all. You are running because it observed something move — the
  task line says what. Its reasons are a gate, never a source of truth: verify
  them fresh, and when they dissolve, close quiet (`quiet: true`) under the
  quiet-beat rule in `## Rules`, which holds here in full.
- **Do not acknowledge the sensor.** The wrapper commits the observation that
  fired you only after you exit cleanly, so an iteration that fails re-fires
  on the same facts. Exiting with an honest report is the acknowledgment.
- **Do not touch `~/.factory/heartbeat/<instance>`.** The wrapper stamps it on
  a clean exit, which means it now records that an iteration *finished* rather
  than that one started.
- **Do not call `scripts/factory-beat.sh`.** The wrapper writes the beat line
  from your report's counters. `quiet` is one of them: `true` on a beat you
  closed early under the quiet-beat rule, `false` on a full pass. Nothing here
  clears a context — each beat is its own process — but the beat log is read
  across runtimes, and a field that means one thing on resident and nothing
  here is a field nobody can total.

Everything else in step 8 is still yours: update `.factory-watermark`, and
compose the status report.

## The report is the return value

Your final output is a JSON object matching the schema you were given, not
prose for a human. The `summary` field carries the status report you would
have posted, `WAITING ON YOU` block first and in the same shape as always —
reception reads it from there.

`waiting_on_you` is the same items as one array entry each, one line apiece,
with the direct URL. An empty array means nothing is waiting, which is the
machine-readable form of saying "WAITING ON YOU: nothing" out loud.

**Both fields, every submission.** The schema requires `summary` and
`waiting_on_you` together, and a submission carrying only the summary is
rejected whole however good the summary was (observed 2026-09-04: three
rejections for the missing array on one beat). A rejection means the shape
was wrong, never the content: resubmit the same report with the shape fixed.
**Never submit placeholder values to see whether the schema accepts them** —
a placeholder that validates becomes the beat's official record, reception
reads it, and the real report is gone.

The counters are what you actually did this iteration, and zero is a fine
answer for all of them.

## Steering still reaches you

Step 0 is unchanged and matters more here, not less: the reception inbox at
`~/.factory/inbox/<instance>/*.json` is now the only way a human reaches you
mid-flight, because there is no pane to type an `INTERRUPT` line into. Drain it
first, every time.

## If you cannot finish

Exit anyway, with a report that says so in `summary`. A half-finished iteration
that reports honestly is recoverable — the next fire reads the world fresh and
sees the same unclosed gap. An iteration that hangs trying to finish is not.
