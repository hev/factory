---
title: Mini SSH has no usable identity from the gaffer host — not actually a host-key problem
date: 2026-09-08
area: infra
kind: environment
tags: [ssh, mini, publickey, credential]
source: https://linear.app/hevmind/issue/FAC-11/kit-dashboard-loads-in-a-second-and-says-so-filters-without-a-search
---

## What happened
FAC-11 step 9 (reindex, gaffer-owned) needs to SSH to the mini
(100.126.12.95) to run live verification. Six+ beats reported this as a
"host-key verification" failure and re-surfaced it as a fresh `[human step]`
every time (first reported ~2026-09-07T22:29Z, still open 2026-09-08T02:05Z).

Direct reproduction on 2026-09-08 showed the real cause is different:
`~/.ssh/known_hosts` on the gaffer host had no entry for the mini at all
(not stale — absent), so plain `ssh` with default `StrictHostKeyChecking`
failed before ever reaching auth, and every prior beat likely misread that as
"host-key verification failing" without adding the key and looking past it.

## What didn't work
- Retrying the SSH call on a later beat — nothing about a missing
  known_hosts entry changes on its own.
- Reporting it as "host-key verification fails" every beat — nobody added
  the key and looked at the *next* error, so the real blocker (no identity)
  never surfaced.

## What works
`ssh -o StrictHostKeyChecking=accept-new -o BatchMode=yes 100.126.12.95 true`
gets past the host-key step cleanly (adds the key, non-interactive). What's
left is a real credential gap: `ssh-add -l` on the gaffer host reports "The
agent has no identities" and `~/.ssh/` has no private key file — this
account has no SSH identity configured for the mini at all. That part is a
genuine `[human step]`: the operator needs to add a key (or agent-forward
one) authorized on the mini for this account before step 9 can run.

## How to avoid it
Before treating an SSH failure to the mini as "host-key," run with
`-o StrictHostKeyChecking=accept-new -o BatchMode=yes` first and read the
*actual* resulting error (publickey vs host key vs timeout) rather than
assuming. If it comes back `Permission denied (publickey,...)`, that's a
credential `[human step]`, not a host-key one — file it once and stop
re-surfacing it as the wrong diagnosis every beat.
