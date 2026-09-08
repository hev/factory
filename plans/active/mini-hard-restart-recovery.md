# The mini comes back from a hard restart unattended

> Source: https://linear.app/hevmind/issue/FAC-13/the-mini-comes-back-from-a-hard-restart-unattended

Filed by reception on Adam's ask, 2026-09-08: "anything we should do to bring back up the mac mini from hard restarts better? … if there are specific things to do, let's do them." Learnings are on [FAC-2](https://linear.app/hevmind/issue/FAC-2/the-mini-is-one-repo-hevlab-declares-every-stack-plist-and-loop-the).

As the operator, I want a hard restart of the mini to end with Colima, the three layer-pro runners and every declared stack back without a human, and a hung mini to page me within fifteen minutes, so that the next 00:07-to-02:16 outage is a fifteen-minute one.

## Acceptance criteria

* `sudo reboot`, and separately a forced power cycle, ends with `lab doctor` green and `gh api repos/hev/layer-pro/actions/runners` reporting all three `online` within 10 minutes of boot, with nobody logged in interactively.
* A simulated hang (`colima stop -f`, or pausing the heartbeat) pages within 15 minutes through the heartbeat monitor.
* `lab converge` on a healthy running box is a no-op, and FAC-2's test 3 (`colima stop`, then `lab converge`) passes.

## Work list (all in `hev/lab` unless noted)

1. **Runtime converge at boot and on a timer.** `launchd/com.hev.runtime.plist` (RunAtLoad, StartInterval 900) runs `host/bin/runtime-converge.sh`; installed by `launchd-converge.sh` like every other plist. Drafted on the laptop's `plan/mini-is-one-repo` checkout, uncommitted. *Accept:* after a reboot `/tmp/runtime-converge.log` shows Colima and the stacks up with no login; `launchctl list` carries the label; doctor knows it.
2. **Pin the root disk.** `--root-disk 150` on the `colima start` line in `runtime-converge.sh`, with the why in a comment (drafted, uncommitted). *Accept:* `colima stop && lab converge` works. Optional follow-on, decided not drifted into: recreate the instance with the default 20 GiB root plus the 150 GiB runtime disk. That loses build caches and runner registration volumes and needs `lab up ci-runners` to re-register.
3. **Restart on freeze.** `systemsetup -setrestartfreeze on` next to the pmset line in `host/defaults.sh` (drafted, uncommitted). **[human step]** run it once with sudo on the box now. *Accept:* `systemsetup -getrestartfreeze` reports On.
4. **Runners tolerate a crash-restart.** After an ungraceful stop the three runners loop on "A session for this runner already exists" (ci-runners README). Make the entrypoint back off and retry until GitHub's session expires, or have `runtime-converge.sh` do `lab down ci-runners`, wait until GitHub reports all three offline, then `lab up`. *Accept:* `colima stop -f; lab converge` brings all three online with no manual down/up.
5. **Disk headroom.** A weekly `docker builder prune --keep-storage 10g` plus `docker image prune` in the ci-runners stack (or its own plist), and doctor's threshold raised so it fails before the runtime disk's remaining ~22 GB of growth can fill the host. *Accept:* doctor fails under 60 GB free; the prune job's log shows it ran and what it freed.
6. **The off-box signal works.** doctor reports the heartbeat stale/never sent and the Datadog agent not running. Cause found for the heartbeat, 2026-09-08 02:26Z: `heartbeat.sh` reads `op://layer-factory/factory-heartbeat/url` and 1Password answers that `factory-heartbeat` is not an item in the `layer-factory` vault, so the dead-man's-switch has never pinged. **[human step]** create that item with the monitor's ping URL (or point the script at the item that exists). Datadog: `datadog-agent` is not on PATH and the agent plist is loaded but not running. Fix both, and confirm the heartbeat monitor pages on a fifteen-minute miss. *Accept:* doctor green on both; a deliberate 20-minute pause of `com.hev.heartbeat` produces a page.
7. **Say so.** `stacks/ci-runners/README.md` and `host/README.md` gain a "hard restart" section pointing at 1–6, and a learning under `docs/learnings/`. *Accept:* a reader following `host/README.md` after a power cycle has nothing to do by hand.
8. **Size the VM for the builds.** The mini is a 12-core / 64 GB M4 Pro; Colima runs 8 CPU / 24 GB and three CI runners share it, which is the other half of why the layer-pro Rust job takes an hour (the layer board RFC "layer-pro CI: the Rust job in under ten minutes, on Depot" owns the workflow side). `colima start --cpu 10 --memory 40 --disk 150 --root-disk 150` in `runtime-converge.sh` (drafted, uncommitted, 2026-09-08). Applying it needs a VM stop/start, which drops running CI jobs; do it between runs. *Accept:* `colima list` shows 10 CPU / 40 GiB; three concurrent Rust jobs finish without an OOM kill; the host keeps at least 16 GB for the harness.

## Out of scope

The hang's root cause (no panic log; disk pressure is the likely suspect, unproven). Any change to the factory's own plist or `~/.factory` state, which stay the factory's. Moving the runners off the mini.

## Operator steering and remaining acceptance — 2026-09-08

The operator canceled the scheduled reboot/power-cycle/alert drill request: https://linear.app/hevmind/issue/FAC-22. Do not schedule or re-file a drill. Source is merged in https://github.com/hev/lab/pull/4. The last unplanned restart recovered headlessly; this is an operator observation, not a measured acceptance drill.

9. **Finish local monitoring readiness.** Diagnose the heartbeat reference using only the existing vault grant and repair the reference if an existing item resolves; diagnose Datadog and the unloaded serve job locally. Keep secrets out of output and repos, never create credentials or send a test alert. Validate with the local doctor and fixtures; preserve any remaining privileged checks explicitly.
10. **Observe the next real restart.** After the next naturally occurring restart, read boot/runtime/doctor logs and record elapsed recovery and remaining failures against the success measure. No forced restart, power-cycle, heartbeat pause or new drill request. This acceptance remains pending until that event.
