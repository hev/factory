# Shared build caches: installed acceptance, 2026-09-09

[Authoritative plan](https://github.com/hev/factory/blob/main/plans/active/worker-shared-build-caches.md),
integration steps 1–5 and the one-week measure. This records partial installed
acceptance after PR18/PR20; it does not close the plan.

| Check | Observation | Remaining acceptance |
| --- | --- | --- |
| Installed launcher | Factory `0ec134a`; role wrapper, cache wrapper and cleanup helper match this checkout byte-for-byte. Direct and disposable tmux worker probes inherit `hev--factory`, report role `worker`, and cannot acquire an exclusive lease. | Cache already existed at first probe (initial dispatch preflight reported it absent). Creation from absence was not independently observed. Other active shared holders remain; no lease was removed. |
| Representative scoped build | Actual factory `go build -x -o <evidence>/factory-build ./cmd/factory` through the installed worker wrapper: primary checkout 1.531 s, linked worktree 1.566 s, both exit 0. Same default shared GOCACHE; second trace reads cached dependencies and recompiles nine local packages, with no dependency compiler invocation. No worktree `target/` created. | This is Go reuse, not the plan's representative unchanged Rust build across workers. No Rust workload was fabricated; retain that tail for an authorized repo that actually builds Rust. |
| Harvest | Installed helper's scoped dry-run exits 0; factory has no recorded candidate directory. | Empty dry-run proves no removal. Observe a future eligible merged-at-recorded-HEAD, clean, unlocked, unused candidate; retain closed-unmerged work. No retrospective candidate was invented. |
| Maintenance / doctor | Installed lab remains clean at `239e940`; CLI and loaded weekly job resolve into that checkout. Cargo helper, prune integration and doctor budget call are absent. Job interval is 604800 s; zero runs observed. Fetched main `725d46a` contains the policy. | Installed eviction and budget doctor remain unavailable until rollout. No maintenance, scheduler changes or global doctor were run. |
| Source doctor on actual disk | Extracted fetched-main helper, read-only `doctor`: exit 0, 47,030,661,120 bytes versus 107,374,182,400-byte limit. | Source execution on real disk is not installed doctor acceptance; prior red/green and eviction fixtures remain [separate lab evidence](https://github.com/hev/lab/blob/0103759/docs/evidence/shared-build-caches.md). |
| Contract | Installed checkout includes merged contract changes; this one-shot loaded them per dispatch. | No contract edit or service restart in this pass. |

Baseline at **2026-09-09 16:44:50 UTC** (allocated KiB from `du`, free KiB
from `df`): all worktrees 45,928,380 KiB (**43.801 GiB**); worktree targets
39,775,780 KiB (**37.933 GiB**); shared Cargo root 0 allocated KiB;
data volume free 109,137,768 KiB (**104.082 GiB**). The target bound is already
above 20 GiB. A single free-space sample cannot establish a 60 GiB minimum
throughout a week. Earliest seven-day comparison is September 16 at 16:44:50 UTC;
a week of normal dispatch after complete rollout still needs timestamped samples
and a recorded minimum. No week elapsed in this acceptance pass.

## Reproduction and rollout handoff

Private commands, paths and raw logs live at
`~/.factory/evidence/factory/worker-factory-cache-installed-0909/`:
`accept.py`, `acceptance.json`, `probe.py`, `tmux-check.py`, `tmux-probe.json`,
`build-1.log`, `build-2.log`, `build-3.log`, `recheck.json`, `observe.py`, `baseline.json`,
`cleanup-dry-run.log`, `source-build-caches.py`, and `update-source.sh`.
The JSON records include commands, exit codes and source revisions. These probes
and build logs stand in for browser evidence; this task has no UI surface.
Final acceptance rerun: direct/tmux probes pass; factory build exits 0 in
1.551 s (one local main-package compile after the documentation edit).

Portable checks from an authorized factory checkout (use the installed checkout
for the wrapper and a private evidence directory for build output):

```sh
scripts/factory-as.sh worker -- python3 "$EVIDENCE/probe.py"
scripts/factory-as.sh worker -- go build -x -o "$EVIDENCE/factory-build" ./cmd/factory
python3 "$EVIDENCE/tmux-check.py"
# Use Python 3.11+, as the installed reaper's PATH does:
python3 scripts/factory-clean-worktrees.py sweep factories/factory.toml \
  "$HOME/.factory/harvest/factory/worktrees" --dry-run
python3 "$EVIDENCE/observe.py"
```

The existing lab `host/update.sh` fast-forwards declared checkouts, upgrades the
factory binary, builds the board and restarts the floor. It is host-wide and
outside this worker's authorization. Its exact installed and fetched procedure
was inspected and retained privately; the lab primary checkout was not changed.
Fetching lab main succeeded: no missing repository-read access was observed.
Deployment access was not exercised; the outstanding action is parent/operator
rollout coordination, not a newly demonstrated credential failure.

After authorized rollout, verify the lab revision, CLI resolution, helper bytes,
prune call, doctor call and loaded weekly job's script path/interval. Run the
installed cache-only `host/bin/build-caches.py doctor`; observe a scheduled
maintenance log showing idle eviction and active retention. Do not invoke the
combined prune job just to test caches. Preserve lease inodes and coordinate any
host-wide action with the parent. Representative Rust reuse, real eligible
harvest removal and the week-long measures stay on the existing plan.
