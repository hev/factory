---
title: Per-worker build output consumes disk after pull requests merge
date: 2026-09-08
area: workers
kind: environment
tags: [cargo, disk, worktrees, tmux]
source: https://github.com/hev/factory/blob/main/plans/active/worker-shared-build-caches.md
---

## What happened
Repeated worker builds left tens of gigabytes in each worktree. Merged work
continued occupying disk after its session disappeared; disk pressure recurred.

## What didn't work
Looking only at container storage missed the source worktrees. Ending worker
sessions did not remove their files. Exporting a shared target in the parent
alone is also insufficient: tmux constructs its session environment separately.

## What works
Pass the target explicitly through tmux and share it across workers of one
repository. Preserve a cleanup candidate beyond session harvest, then verify
the PR merged at the recorded HEAD, the worktree is clean, and no session or
process is using it. Keep shared caches independent of individual PR lifetimes.

## How to avoid it
Measure both worktrees and shared targets. Test launches across two worktrees,
and test the later merge after the original worker session has disappeared.
A weekly maintenance pass cannot guarantee free space during a live build.
