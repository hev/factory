# AGENTS.md — working in hev/factory

This repo is the `factory` CLI: background coding sessions on machines you
own, coordinated by a model on the laptop. It also holds the reception skill
that teaches that model the CLI, and hevfactory.com (`site/`). Public,
Apache-2.0.

```
cmd/factory/        the CLI: hosts, run, ls, peek, send, wait, attach, kill, find, skill
internal/fleet/     sessions, the runner, host info, placement, the ssh wire
internal/tmuxctl/   the thin layer over tmux
internal/auth/      login expiry, read from the files each login writes
skills/reception/   the coordinator skill, embedded and installed by `factory skill install`
site/               hevfactory.com
evals/rubric.md     what a finished session is graded against
```

## What does not belong here

Nothing about one estate: no hostnames, vault references, Slack webhooks,
GitHub accounts or provisioning. Those live in the private overlay
(`hev/factory-pro`) and in the host's own configuration (`hev/lab`). The
dependency runs one way: **this build never learns the overlay exists.**

The overlay reaches the CLI through two executable names, and nothing else:

- **`factory-<verb>` on PATH** answers `factory <verb>` for any verb this
  binary does not own (`factory board` is `factory-board`).
- **`factory-brief` on a host's PATH** adds context to every session's first
  turn. `run` starts it in the new worktree with the task on stdin, and
  puts what it prints into the brief.

Keep both satisfiable by hand, with a shell script that prints text. If a
change makes either work only with something someone has to buy, it is the
wrong change.

## Harness-agnostic

A session runs on claude or codex, chosen per run. Everything a session needs
goes into its prompt. Don't rely on one harness's hooks, settings or skills,
because the other harness never sees them. `turnCommand` and
`interactiveCommand` in `internal/fleet/harness.go` are the only places that
name a harness's flags.

## Who a session acts as

A session acts as whoever its host is logged in as: `gh`, git author and
subscriptions. The CLI never passes an identity along, and never lets a
session borrow one from the host that asked for it.

## Where this runs

It's developed on a laptop and run on every host, and every host needs the
same build, because the laptop runs `factory _host` on the others over ssh.
`hev/lab`'s `host/update.sh` builds it from the pulled checkout on the
always-on host. A running turn keeps the binary it started with; the next
turn runs the new one.

## Checks

`go vet ./... && go test ./...`. For anything that changes how a session
starts or resumes, run one for real. Use a scratch `FACTORY_HOME` and a cheap
model, and `factory kill --rm` it afterwards: sessions run with every approval
off.
