# Replay the producer's actual field shapes

A synthetic eval with string findings passed `hev eval put`, while the
historical factory producer emitted finding objects and failed decoding.
Checking only required keys (`session`, `ts`, `marks`) missed the mismatch.

Inspect field types without printing private prose, freeze the input, and
replay it twice through the public adapter/CLI into a disposable store.
Factory's `scripts/costs/eval_rows.py` preserves finding objects as canonical
JSON strings. Check exact unique IDs and unchanged stored rows, not just a
successful exit or the number of submitted rows. Keep production rollout
acceptance separate from an HTTP contract fixture.
