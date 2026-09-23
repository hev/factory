# Local dispatch and plan bookkeeping rollout

This change adds local event dispatch, deterministic approved-plan publication,
and per-task capacity/dependency reporting. It does not migrate task histories
or change worker limits, approvals, merge grants or product review requirements.

## Before installation

Run from the candidate checkout:

```sh
python3 -m unittest discover -s tests -p 'test_*.py'
go test ./cmd/factory ./internal/ciwatch
git diff --check
```

Deploy the reviewed commit through the installation's normal update mechanism.
Include `scripts/factory-bookkeeping.py` and rebuild the `factory` CLI for the
CI wake hook. Restart the foreman and affected gaffers after active turns have
finished so they load the changed role contracts. Preserve workers, holds,
approval receipts, task histories and existing worktrees. Do not start a second
controller from a development checkout or bypass `home_host`.

## Installed-host acceptance

Use an approved test assignment on the configured home host:

1. Record commission completion, then confirm its first eligible worker starts
   before the next external intake poll. Inspect `controller/local-dispatch.json`
   and `runner.log` for errors.
2. Observe an intermediate `done` fact advancing its dependent review without
   another remote intake sweep. A registered pending CI watch must still block
   advancement. Observe a CI poll releasing that handoff when checks pass.
3. With one repository at two workers, confirm its status names that repository,
   count, limit and worker IDs. A ready task in a different repository/lane may
   start if global capacity permits. Confirm status clears after capacity frees.
4. For a newly ingested unchanged plan, have its owner prepare a dedicated clean
   linked worktree at `origin/<plans_branch>` with branch `bookkeeping/<session>`.
   Run `bookkeep <session> <worktree> --publish` during its acknowledged event
   turn. Verify the single-file diff, exact source bytes, receipt and PR URL.
   No copy/review model workers should be commissioned. Apply existing CI and
   merge gates to the PR; its unmerged state does not block product dispatch.
5. Observe three scheduled polls. Confirm held/paused assignments do not launch,
   attempted failures remain blocked, and quiet queues create no model turns.

Older assignments lack the pinned intake digest. The helper refuses them;
handle their existing bookkeeping through owner judgment. Do not mark existing
bookkeeping tasks complete or synthesize pins to make rollout acceptance pass.

## Recovery

The durable spool and CI watches remain authoritative if a wake fails. Scheduled
polls retry observation, never failed model inputs. `dispatch` can also run a
local pass on the home host; it exits nonzero when observation fails. Inspect
the saved problem before retrying.

Bookkeeping receipts preserve the intended base, branch, source and digest
before Git writes. Replay resumes an exact interrupted copy or reuses its PR;
different bytes, unrelated files or conflicting PR heads fail without a reset
or force push. Resolve those cases explicitly in the owning turn.

To roll back, stop new manager turns, let active turns finish, install the
previous code and CLI through the normal update mechanism, then restart roles
with the matching contracts. Preserve new bookkeeping receipts and PRs for
manual follow-through. They do not confer approval or merge authority.
