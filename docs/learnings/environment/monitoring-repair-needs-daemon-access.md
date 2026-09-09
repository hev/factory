---
title: A configured monitoring service can lack both its input and daemon access
date: 2026-09-08
area: environment
kind: environment
tags: [monitoring, preflight, permissions]
source: https://github.com/hev/factory/blob/main/plans/active/mini-hard-restart-recovery.md
---

## What happened
Repeated recovery checks found a declared monitor whose existing input could
not resolve and a loaded daemon unable to traverse its own log directory.
Source fixes passed while installed monitoring remained unhealthy.

## What didn't work
A loaded service label and a successful start request did not establish a
running daemon. Retrying the missing input or redispatching the same repair
could not supply an absent reference or a privilege the worker did not have.
Old logs described earlier failures, not the current startup failure.

## What works
Check the existing input lookup by exit status without printing its value.
Inspect current service state and the daemon user's access to log directories.
Test the exact required privileged operation non-interactively before promising
an unattended repair; preserve its failure as the operator's concrete ask.

## How to avoid it
Before dispatching monitoring recovery, verify the configured input resolves,
the daemon can traverse its diagnostic paths, and the required privilege is
available. Keep source verification separate from installed-service acceptance.
After a repair, read fresh diagnostics and service health before clearing the
blocker. Do not create replacement credentials or send test alerts as a probe.
