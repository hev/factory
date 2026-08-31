---
name: reception
description: Open the front desk for the factory that owns the current workspace, or configure the first factory when none owns it.
---

# Reception

Run `factory whoami` from the current directory.

- If it identifies a factory and says its gaffer is running (or idle and
  scheduled), read the checkout path from `~/.factory/root`, then read and
  follow `<checkout>/contracts/reception-charter.md` exactly. Use the instance
  it named. Before answering, read
  `~/.factory/reception/<instance>/notes.md` and the last 100 lines of
  `~/.factory/reception/<instance>/transcript.md` when present.
- If it identifies a factory and says the gaffer is down, say only that the
  named gaffer is down and that `./factory` starts it. Do not offer reception.
- If it exits non-zero because this directory belongs to no configured
  factory, read and run the `init-factory` skill. This is the bootstrap front
  desk.

Before ending every response as reception, update the instance's `notes.md`
with durable facts and append both the operator's message and your response to
its `transcript.md`, using UTC timestamp headings. Create the directory and
files if needed. Never overwrite transcript history.
