# Worker build caches on the mini: one cargo target per repo, cleaned when the PR merges

> Source: https://linear.app/hevmind/issue/FAC-19/worker-build-caches-on-the-mini-one-cargo-target-per-repo-cleaned-when

> Filed by reception 2026-09-08 18:30Z after the second disk emergency in a day. Backlog until Adam moves it to Todo.

**What happened.** At 18:18Z the mini's data volume was at **98%, 13 GiB free**, down from 55 GiB at 12:12Z and 81 GiB at 02:16Z, with three lyr workers and the three CI runners live. That is the condition that preceded the 2026-09-07 hang (<issue id="dd478dc0-7169-47ea-afe7-38133f28badd" href="https://linear.app/hevmind/issue/FAC-2/the-mini-is-one-repo-hevlab-declares-every-stack-plist-and-loop-the">FAC-2</issue> learnings). The cause was not Docker: it was **cargo** `target/` **directories in the factory's git worktrees on the host**. Every worker builds the full layer-pro workspace in its own worktree, nothing is shared, and nothing removes the cache when the PR merges. Measured:

| Worktree | PR | target/ |
| -- | -- | -- |
| `.worktrees/layer-pro/lyr20-step3-conformance-matrix` | #560, merged | 44 GB |
| `.worktrees/layer-pro/lyr20-phase1-backend` | #559, merged | 31 GB |
| `.worktrees/layer-pro/namespace-purge-background-steps-6-8-9` | #553, open | 25 GB |
| `.worktrees/layer-pro/lyr14-rest-auth` | #556, merged | 19 GB |
| `.worktrees/layer-pro/lyr34-ce-mirror-pgvector` | #566, merged | 10 GB |
| `layer-pro-lyr35-compose-pull` (stray checkout) | #569, merged | 8 GB |
| `.worktrees/layer-pro/lyr33-steps3-5`, `lyr35-compose-repair` | #570, #569, merged | 7 GB each |
| `~/workspace/layer-pro/target` (primary checkout) |  | 96 GB |

Reception deleted the seven merged-PR caches after confirming no process had a working directory inside them: **126 GB freed, 71% used after.** The primary checkout's 96 GB and the open PR's 25 GB were left alone. The Colima runtime disk is a separate 117 GB and is <issue id="d7732a3c-1ef6-4e80-9aca-30df3c26b641" href="https://linear.app/hevmind/issue/FAC-13/the-mini-comes-back-from-a-hard-restart-unattended">FAC-13</issue> step 5's business.

**As the operator**, I want worker builds on the mini to share one compiled cache per repo and to leave nothing behind when their PR merges, **so that** disk never becomes the reason the box hangs.

## Acceptance criteria

* After a week of normal dispatch, `du -sh ~/workspace/.worktrees/*/*/target` on the mini totals under 20 GB, and the data volume never drops below 60 GB free.
* A worker's second build of the same repo reuses compiled dependencies from the previous worker's build (measured: `cargo build` on an unchanged workspace completes in under 2 minutes).
* Harvesting a worker whose PR is merged removes its worktree and its build cache.

## Work list (in `hev/factory` unless noted)

1. **One target dir per repo for workers.** The worker launcher sets `CARGO_TARGET_DIR=$HOME/.cache/cargo-target/<repo>` (and the equivalent for Go: `GOCACHE` is already shared by default). Worktrees then hold source only. *Accept:* two consecutive workers on layer-pro share the directory and the second's cold build is a warm build.
2. **Clean on harvest.** `factory-reap.sh` (or the harvest step that already writes the ledger) removes the worktree, and any `target/` under it, when the worker's PR is merged or closed; it keeps the worktree while the PR is open. *Accept:* the merged-PR worktrees listed above would have been gone within one beat of their merge.
3. **Bound the shared cache.** A weekly `cargo sweep --maxsize 40G` (or `--time 7`) over each shared target dir, in the same lab-declared job as <issue id="d7732a3c-1ef6-4e80-9aca-30df3c26b641" href="https://linear.app/hevmind/issue/FAC-13/the-mini-comes-back-from-a-hard-restart-unattended">FAC-13</issue> step 5's docker prune. *Accept:* the shared dir never exceeds 40 GB; the job's log shows what it removed.
4. **Doctor knows about it.** `lab doctor` fails when `~/workspace/.worktrees` plus the shared target dirs exceed 100 GB, naming the biggest offenders. *Accept:* doctor red at the state measured today, green after cleanup.
5. **Say so.** The worker contract in `contracts/` names the shared target dir and the harvest rule, and `docs/learnings/` carries this incident. *Accept:* a worker reading the contract does not set its own target dir.

## Out of scope

The Colima runtime disk and docker prune (<issue id="d7732a3c-1ef6-4e80-9aca-30df3c26b641" href="https://linear.app/hevmind/issue/FAC-13/the-mini-comes-back-from-a-hard-restart-unattended">FAC-13</issue> step 5). Moving builds to Depot (<issue id="75c0219f-7379-4b2b-acf0-c4a47f44d526" href="https://linear.app/hevmind/issue/LYR-36/layer-pro-ci-the-rust-job-in-under-ten-minutes-on-depot">LYR-36</issue>, <issue id="49560a9c-70ab-456f-b043-497704408aa8" href="https://linear.app/hevmind/issue/FAC-17/builds-run-on-depot-the-mini-runs-the-harness">FAC-17</issue>), which shrinks but does not remove local worker builds, since workers still compile to verify their own work.


## Integration and acceptance follow-through — 2026-09-09

The cache policy formerly in https://github.com/hev/factory/pull/16 is preserved in the combined operator-only https://github.com/hev/factory/pull/18; the superseded branch remains available because it closed unmerged. Implementation remains https://github.com/hev/factory/pull/17.

- Steps 1–2 and 5 still require the combined contract merge, source rollout and representative build/merged-worktree checks; disposable warm-build fixtures are not production acceptance.
- Steps 3–4 remain gated by https://linear.app/hevmind/issue/FAC-24. Parking that unanswered ask does not change the continuous-cap criterion or approve weekly idle eviction.
- After rollout, measure the original one-week worktree and free-space bounds before archiving. Retain active and closed-unmerged work.
