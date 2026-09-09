# Worker build storage

The worker role wrapper resolves the launch directory's GitHub `origin` and
sets `CARGO_TARGET_DIR=$HOME/.cache/cargo-target/<owner>--<repo>` in lowercase.
The owner component prevents collisions between repositories with the same name.
Go keeps its default shared `GOCACHE`. Python 3.11+ is required for cleanup.

The standard `tmux new-session ... -c <repo>` launch opens an interactive login
shell holding a shared maintenance lease at `cargo-target/.locks/<owner>--<repo>`.
Submit the harness in that shell as usual. Worker tmux shell-command variants
are rejected; flags must be separate arguments. Direct worker commands hold
the same lease. Keep the inherited Cargo target when creating a worktree.

Harvest saves candidates in `~/.factory/harvest/<instance>/worktrees/`, separate
from the parent-owned live child ledger. Set the optional ledger `worktree` to
the canonical linked worktree path, especially if the session may exit before
harvest; otherwise harvest resolves the pane directory. Missing metadata does
not authorize scanning unrelated material. Old harvests are not retroactively
swept. Retain this directory while candidates remain.

Each reaper pass checks candidates against the current factory's exact
`repo_scope` and exclusions and queries only that repository's PR. Removal
requires MERGED, matching PR/recorded/current HEAD, no worker session, no live
pane or observable process using the tree, and no modified, untracked, or
ignored files except ignored `target/` output. Locked worktrees remain intact.
Uncertain probes fail visibly and preserve the candidate. Closed-unmerged
work stays. `--dry-run` never writes or removes candidates. No branch or shared
target is deleted by harvest. The process probe requires `lsof` visibility;
this is conservative cleanup on a cooperative worker host, not protection
against a concurrent external writer deliberately racing the final checks.

Shared-target maintenance takes the same lease exclusively and must never
unlink lease files. An active cache can grow past a budget; report that failure
and defer eviction until the worker exits. The host scheduler owns maintenance.
Contract rollout is separately operator-gated; restart the gaffer after it lands.
