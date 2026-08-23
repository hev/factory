# `runtimes/<name>.sh` — how a beat runs

An instance's `runtime` field picks how its gaffer's iteration is executed.
Two are built in and resolve to scripts in the repo root rather than to files
here:

- `resident` → `../factory-up.sh`, a claude session kept alive in tmux, the
  agent scheduling its own next beat.
- `one-shot` → `../factory-iterate.sh`, one `claude -p` process per beat with
  launchd owning the loop.

Anything else resolves to `runtimes/<name>.sh` in this directory, called with
the instance name as its only argument. That is the seam for a runtime this
build does not ship — a beat that runs somewhere other than this Mac.

The contract is small enough to state completely:

| | |
|---|---|
| **called as** | `runtimes/<name>.sh <instance>` |
| **reads** | `factories/<instance>.toml` |
| **exit 0** | the beat ran, or there was nothing to do |
| **exit 78** | not this machine's factory (`home_host`), which is not an error |
| **any other** | reported by `./factory` and otherwise left alone |

A config naming a runtime with no script gets one clear line at boot and the
other factories keep going. See [`../contracts/extending.md`](../contracts/extending.md).
