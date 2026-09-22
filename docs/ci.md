# Pull request CI

`.github/workflows/ci.yml` runs on pull requests opened, reopened or updated.
It uses a disposable GitHub-hosted Ubuntu 24.04 runner, Python 3.13 and the Go
version in `go.mod`. Git, jq and standard Unix tools come from the hosted image;
the dependency step fails if required tools are missing. Python tests use only
the standard library. Fixtures isolate state and stub external services and
terminals; the workflow does not start the installed factory.

The checkout uses `github.event.pull_request.head.sha`, and a failing equality
check against `git rev-parse HEAD` gates all tests. The revision log and job
summary record both the tested head and GitHub's event SHA/ref. For
`pull_request`, that event SHA normally describes the synthetic merge commit;
it is not the revision tested here. This workflow verifies source at the head,
not compatibility with unintegrated base changes.

The commands, also runnable locally with Python 3.11 or newer, are:

```sh
python3 -m unittest discover -s tests -p 'test_*.py' -v
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
go test ./...
go vet ./...
```

Discovery includes controller, dispatch, reaper, legacy recovery and fairness
fixtures, plus future `test_*.py` lifecycle fixtures integrated into these trees.
The standalone Rust cache fixture `tests/shared-build-caches.py` is outside
unittest discovery and is not part of this workflow. Bash's explicit Actions
shell uses `-e -o pipefail`, so logging through `tee` preserves test failures.
The `exact-head-ci-<head SHA>` artifact retains revision and test logs for 14 days,
including available logs when a step fails. Local results are not CI results.

The existing factory watcher reads the PR head before and after `gh pr checks`
and records check names, buckets and links. It does not inspect the checkout or
artifact: acceptance must compare the immutable tested SHA in the run evidence
with the watched PR head. GitHub's merge-ref metadata alone is insufficient.
Missing checks remain waiting; no synthetic status or watcher exception is used.

A mergeable PR introducing this workflow can run it before merge via the
`pull_request` event. Repository policy or fork approval can still prevent a run;
inspect the actual run and PR checks rather than assuming execution. There is no
`workflow_dispatch` bootstrap (that trigger requires the workflow on the default
branch), privileged `pull_request_target`, release trigger or secrets access.
The separate tag-triggered release workflow is unchanged.

After operator approval and merge of this CI proposal, a separately authorized
integration of a waiting change onto the updated base must create a supported
pull-request event and register that resulting head for CI. Installing this
workflow does not retroactively verify an older PR head or satisfy that change's
functional acceptance criteria.

References: [GitHub pull-request events](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#pull_request),
[checkout head example](https://github.com/actions/checkout#checkout-pull-request-head-commit-instead-of-merge-commit),
[Ubuntu runner inventory](https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md).
