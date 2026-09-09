# Shared build caches acceptance

Local disposable fixtures only; no deployment, live cleanup, or week-long disk
measurements. All commands exited zero. FAIL lines below are expected threshold
assertions, not a failed acceptance run. Temporary paths are normalized.

Python 3.11+ is required by factory cleanup; these checks put that interpreter
on PATH explicitly. No deployed preview is configured for this CLI work.

```text
Fixture acceptance on macOS; Python 3.14.7

$ go test ./...
ok  	github.com/hev/factory/cmd/factory	0.746s
ok  	github.com/hev/factory/internal/auth	(cached)
ok  	github.com/hev/factory/internal/machine	(cached)
ok  	github.com/hev/factory/internal/picker	(cached)
?   	github.com/hev/factory/internal/stopline	[no test files]
?   	github.com/hev/factory/internal/tmuxctl	[no test files]
ok  	github.com/hev/factory/internal/ui	(cached)
ok  	github.com/hev/factory/pkg/factory	0.423s
Exit: 0

$ python3 tests/shared-build-caches.py
PASS: two worker worktrees reuse compiled dependency; second build 0.084s
PASS: real worker holds shared lease, survives supervisor SIGINT, releases on completion
PASS: tmux receives target environment and session lease wrapper
PASS: merged cleanup removes source + ignored target; retains live/attached/dirty/open/closed-unmerged/probe failures; dry-run preserves all; shared cache retained
PASS: reaper integration preserves attached/dry-run; harvest archives pane+ledger; later beat removes merged worktree after live ledger is gone
Exit: 0

$ python3 -m unittest discover -s scripts/tests -p test_preview_browser.py
.....
----------------------------------------------------------------------
Ran 5 tests in 2.084s

OK
Exit: 0

$ bash -n scripts/factory-as.sh scripts/factory-reap.sh
Exit: 0

$ git diff --check
Exit: 0
```
