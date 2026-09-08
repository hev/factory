---
title: Mini SSH fails host-key verification, blocking gaffer-run verification steps
date: 2026-09-08
area: infra
kind: environment
tags: [ssh, mini, host-key, verification]
source: https://linear.app/hevmind/issue/FAC-11/kit-dashboard-loads-in-a-second-and-says-so-filters-without-a-search
---

## What happened
FAC-11 step 9 (reindex, gaffer-owned) needs to SSH to the mini to run live
verification. Every attempt fails host-key verification. This has now
recurred across 6+ beats (first reported ~2026-09-07T22:29Z, still failing
2026-09-08T02:05Z) with no change, each beat re-surfacing it as a fresh
`[human step]` in the WAITING ON YOU block.

## What didn't work
- Retrying the SSH call on a later beat — the host key state does not change
  on its own.
- Repeating the same `[human step]` line every beat — it cost a block slot
  each time without prompting a fix, which is what the no-lingering rule
  exists to stop.

## What works
Not yet resolved. This is filed so preflight (step 2) surfaces it once,
explicitly, rather than a worker or the gaffer discovering the same failure
cold on a later beat.

## How to avoid it
Before dispatching or attempting any step that SSHes to the mini, check
`ssh -o BatchMode=yes <mini-host> true` first. If it fails on host-key
verification, do not retry blindly — this is a `[human step]`: the operator
needs to refresh `~/.ssh/known_hosts` (or fix whatever rotated the mini's
host key) before any mini-dependent verification can proceed. Surface it once
per the no-lingering rule rather than every beat.
