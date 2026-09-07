# The mini is one repo: hev/lab declares every stack, plist and loop the box runs

> Source: https://linear.app/hevmind/issue/FAC-2/the-mini-is-one-repo-hevlab-declares-every-stack-plist-and-loop-the

As the operator of the mini, I want everything the box runs declared in one private repo I can converge from, so that a stopped runner, a drifted plist, or a new docker stack is one commit and one command rather than an archaeology across four repos.

## Why now

The mini's configuration lives in four places today: `factory-pro/host` (bootstrap, converge, Brewfile, launchd, doctor), `lyr/ci-runners` (the three layer-pro runners, shipped to the box as a `mini` job), `kit/deploy` (the hevd plist), and `hevmind-promo/launchd` (four promo plists, rewritten by its `install.sh`). The 2026-08-27 runner outage sat open for a week because nobody could tell what to restart; the ci-runners README says so in its first paragraph. `docker compose` does not work on the box (only the v1 `docker-compose` binary is on PATH), so every runbook line has to spell out `docker-compose -f`. `launchctl list` carries `com.hev.worker-watch` with exit 127 and no plist file behind it. hev loop (its own RFC) needs postgres in docker on this box and has nowhere to declare it.

## Acceptance criteria

* Every label in `launchctl list | grep -E 'com\.hev|com\.hevmind'` on the mini has a source file under `hev/lab/launchd/`, and nothing in `~/Library/LaunchAgents` is hand-edited.
* `lab doctor` exits non-zero when a declared stack or plist is missing or stopped, and exits 0 on the box as it stands after the move.
* `docker compose version` over ssh prints v2. `lab up ci-runners` from the laptop restarts the three layer-pro runners and `gh api repos/hev/layer-pro/actions/runners` reports all three `online` within two minutes.
* `factory-pro/host` and `lyr/ci-runners` are replaced by a three-line pointer each; `git log --follow` in hev/lab shows their history.
* Nothing public changes. hev/lab is private.

## How to test it

1. On the mini, `lab doctor` is green and `launchctl list` matches `lab/launchd/*.plist` by label, one to one, with `com.hev.worker-watch` either declared or unloaded.
2. `lab down ci-runners && lab up ci-runners` from the laptop; runners online within two minutes (the graceful-stop grace period is 60s).
3. `colima stop`, then `lab converge`: colima up at 8 CPU / 24 GiB / 150 GiB, runners up, doctor green.
4. Push to kit `main`: the `deploy-mini` job still installs `hev` and kickstarts `com.hev.hevd` and `com.hev.serve`, so those labels survived the move unchanged.

## The work

1. **The repo.** Create private `hev/lab`. `host/` from `factory-pro/host` by `git subtree split` (history kept). `stacks/ci-runners/` from `lyr/ci-runners`. `launchd/` gathers `com.hev.hevd` from kit/deploy, the four `com.hevmind.promo-loop-*` from hevmind-promo, and `com.hev.{factory,heartbeat,foreman,board}` from factory-pro/host/launchd. *Accept:* criterion 4.
2. **The verb.** A shell script `lab` at the repo root: `lab up|down|ps|logs <stack>` wraps `docker compose -f stacks/<stack>/compose.yml`; `lab doctor` is `host/doctor.sh` extended to check declared stacks and containers; `lab converge` is `host/converge.sh`. Converge installs it to `~/.local/bin`. The hidden `hev loop up|down|logs|ps` compose wrapper in kit (`cmd/hev/loop.go`, pointing at the retired `../loop` MinIO stack) is deleted in the same change. *Accept:* tests 1 and 2.
3. **Compose v2.** Converge links `$(brew --prefix)/opt/docker-compose/bin/docker-compose` into `~/.docker/cli-plugins/docker-compose`. *Accept:* criterion 3.
4. **Plist ownership.** `hevmind-promo/install.sh` stops writing plists and points at lab; kit's Makefile and deploy dir keep the fluent-bit files and lose the plist. *Accept:* `grep -rn LaunchAgents hevmind-promo/install.sh kit/Makefile` finds no writes.
5. **Colima is declared.** Converge starts colima idempotently at the current sizing, then brings every stack up. *Accept:* test 3.
6. **The loop stack, parked.** `stacks/loop/compose.yml` with `postgres:16` (image already on the box) on a named volume, published to `127.0.0.1:5432` and reachable from the runners at `host.docker.internal:5432`. Not started by converge until the loop RFC ships. *Accept:* `lab up loop && psql -h 127.0.0.1 -c 'select 1'`.
7. **Pointers.** `factory-pro/host/README.md` and `lyr/ci-runners/README.md` become three lines to hev/lab; `hevmind-strategy/chief/DIRECTORY.md` Machines row names lab. **[human step]** The factory-pro Linear project summary drops "host provisioning for the mini". *Accept:* the three files read as described.

## Constraints

* No secrets in the repo. Everything resolves from 1Password at the moment of use, as today.
* `lab` is a repo and a verb on this box. It is not a client and gets no formula; `lab` is already a homebrew-core formula name.
* The open-source home lab is kit RFC 0004's `deploy/homelab/` compose and stays in kit. This repo never has to be public.
* `~/.factory/` state, `factory-up.sh`, and the factory's own launchd installation stay the factory's. lab declares the plist file and nothing else.
* Git on the mini runs as hevbot over https (host README). lab is cloned the same way; nothing is rsynced over a workspace.

## Out of scope

The loop service and the tap (separate RFCs). Making the lab a product. Kubernetes of any kind on this box.

## Estimate

Two to three worker sessions. The subtree split and plist moves are mechanical; the verb is a hundred-line shell script; the risk is the plist labels the kit CI job and the factory reference by name, which must not change.
