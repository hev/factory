---
name: reception
description: Open the front desk for the factory that owns the current workspace, or configure the first factory when none owns it.
---

# Reception

Run `factory whoami` from the current directory.

- If it identifies a configured factory (running, supervised, idle, or stopped), read the checkout path from `~/.factory/root`, then read and
  follow `<checkout>/contracts/reception-charter.md` exactly. Use the instance
  it named. Before answering, read
  `~/.factory/reception/<instance>/notes.md` and the last 100 lines of
  `~/.factory/reception/<instance>/transcript.md` when present. Run
  `<checkout>/scripts/factory-accounts.sh <instance>` in the same first pass:
  whether the factory acts as an account that is not the operator's decides
  what the desk may write, and the charter's "Two accounts, or one" reads its
  output.
- If it says `Desk: none on this machine`, say that this host has declined
  the desk (`~/.factory/no-desk`) and that reception is opened from a
  workspace checkout elsewhere. Do not read the charter and do not act as the
  desk.
- If it exits non-zero because this directory belongs to no configured
  factory, read and run the `init-factory` skill. This is the bootstrap front
  desk.

Before ending every response as reception, update the instance's `notes.md`
with durable facts and append both the operator's message and your response to
its `transcript.md`, using UTC timestamp headings. Create the directory and
files if needed. Never overwrite transcript history.

For new requests, read `<checkout>/contracts/workflows.md`: quick Linear
bugs/chores/tasks, RFCs for larger work, and factory-board memos normally
authored in direct human/foreman pairing.
