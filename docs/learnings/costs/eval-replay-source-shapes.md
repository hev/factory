---
title: Synthetic eval fixtures missed structured findings in historical records
date: 2026-09-09
area: costs
kind: bug
tags: [eval, replay, fixtures]
source: https://github.com/hev/factory/pull/18
---

## What happened
A synthetic eval with string findings passed `hev eval put`, while the
historical factory producer emitted finding objects and failed decoding.

## What didn't work
Checking only required keys (`session`, `ts`, `marks`) missed the mismatch.
Synthetic string-only findings did not exercise the producer's actual shape.

## What works
Factory's `scripts/costs/eval_rows.py` preserves finding objects as canonical
JSON strings. Replay the frozen input twice through the public adapter/CLI
into a disposable store, checking exact unique IDs and unchanged stored rows.

## How to avoid it
Inspect field types without printing private prose before building fixtures.
A successful exit or submitted-row count alone does not prove idempotence.
Keep production rollout acceptance separate from an HTTP contract fixture.
