# Reception is a skill, not a session

> **ARCHIVED 2026-08-31** — shipped by hand, all seven steps, released as
> v0.1.2. Reception is a user-level skill invoked by `/reception` or by the
> workspace SessionStart hook; `factory whoami` resolves the instance from
> `$PWD`, `factory adopt` installs the hook in a workspace already configured,
> and continuity lives in `~/.factory/reception/<instance>/`.
> `reception-up.sh`, `launchd/com.hev.reception.plist`, `pkg/factory/reception.go`
> and the picker's reception row are gone.
>
> **The speak-first duty landed as `scripts/floor-watch.sh`**, called by
> `factory` on every fire after the gaffers, with its cursor in `spoken`. The
> desk's judgement about which unprompted line was worth sending is genuinely
> gone, as step 5 said it would be.
>
> **Holds came in alongside**, unplanned by this RFC but forced by it: with
> reception out of the boot path, `./factory` on a 300s fire was the last thing
> undoing an operator's stop. `pkg/factory/holds.go` makes `factory stop` stay
> stopped until `factory up`, per instance, across a reboot.

## What changes

An operator opens Claude in a workspace the factory works on — the repo they
were already in — types `/reception`, and is talking to the front desk of that
factory. No tmux session to attach to, no desk to keep alive, no second place
to be. When they close the window, reception is gone and the factory is
untouched.

Today the desk is a resident tmux session per instance, booted by launchd every
300s. That buys a conversation that persists for weeks, and costs: a claude
process per factory sitting idle, a boot path (`reception-up.sh`, 190 lines) to
keep in step with `factory-up.sh`, a second launchd plist, and a front desk
that lives in the factory checkout rather than in the work.

The decoupling is the point. Stopping the line stops the factory and reception
is unaffected. One operator can open reception in three workspaces against three
gaffers, or none, and the count of desks stops being a property of the machine.

## Why now

`reception-up.sh` is the largest remaining piece of boot machinery that exists
only to keep a conversation alive, and every change to how a factory starts has
to be made twice. Making reception a skill deletes one of the two.

## How success is measured

`./factory` starts gaffers and nothing else; `pgrep -f 'claude.*reception'`
returns nothing on a running factory. Typing `/reception` in a configured
workspace answers "what's waiting on me?" with the same facts the desk gives
today, from the same files. And the three speak-first classes still reach Slack
with the gaffer's launchd fire as their latency bound.

## The wakeup, which is the part that does not survive

The charter's *When you speak first* list is three things: a worker that blocked
between beats, a factory that has gone quiet, a machine problem about to take
the floor down. A skill cannot be woken, so this duty moves — and it turns out
it never needed a model. The first is a `blocked` line in the spool, the second
is `factory-health.sh` exiting non-zero, and the third is what health checks
should have been reporting all along. A watcher on the same fire the gaffers
already take posts all three through `notify.sh` and holds the once-per-thing
cursor in a file.

What is genuinely lost is the desk's judgement about *which* unprompted line is
worth sending. That was worth a model and it is worth saying out loud that it is
going away.

## Steps

1. **Ship the charter as a user-level skill.** `~/.claude/skills/reception/`,
   installed by `./factory`, sourced from `contracts/reception-charter.md` —
   the contract keeps one home, the skill is a thin frontmatter wrapper that
   loads it. Invoked in any directory, not only the checkout.
   *Done when:* `/reception` in a workspace with no factory configured runs the
   `init-factory` conversation, which is the bootstrap desk's whole job today.

2. **Resolve the instance from the working directory.** A skill has no session
   name to read it off. Add `factory whoami` to the picker binary: match `$PWD`
   against every config's `workspace_path`, print the instance, its gaffer
   state, and its plans door — or exit non-zero when the directory belongs to no
   factory.
   *Done when:* run inside `charlie`'s workspace it names `charlie`; run in
   `~/Downloads` it exits non-zero with a sentence saying so.

3. **Make the mode automatic when a gaffer is up.** `factory init` (and a new
   `factory adopt <instance>` for a workspace already configured) writes a
   SessionStart hook into the workspace's `.claude/settings.json` that runs
   `factory whoami`. Its output is the boot prompt: the instance, whether the
   gaffer is running, what is waiting, and the line that tells Claude to load
   the reception skill. Non-zero exit prints nothing and the session is
   ordinary.
   *Done when:* opening Claude in a configured workspace with a live gaffer
   greets as reception without anybody typing a command, and opening it in the
   same workspace with the gaffer stopped says the gaffer is down and offers
   nothing else.

4. **Continuity moves into the files, all of it.** No process survives, so
   `notes.md` stops being a hedge against compaction and becomes the state.
   Reception reads `notes.md` and the tail of `transcript.md` on invoke and
   writes both before it ends a turn, per instance:
   `~/.factory/reception/<instance>/`.
   *Done when:* a second `/reception` in a fresh window knows what the first one
   was told, and the charter's self-compaction ritual is gone.

5. **Move the speak-first duty to a watcher.** `scripts/floor-watch.sh
   <instance>`, called by `factory` on every fire after the gaffers: posts a
   blocked worker's ask, and a factory `factory-health.sh` calls late. One line
   per thing, cursor in `~/.factory/reception/<instance>/spoken`.
   *Done when:* a worker that blocks between beats produces exactly one Slack
   line within one fire, and a second fire with no new spool lines posts
   nothing.

6. **Take reception out of the boot path.** Delete `reception-up.sh`,
   `launchd/com.hev.reception.plist`, `pkg/factory/reception.go`, and the
   picker's ↵-on-a-down-desk. `./factory` starts gaffers, installs the skill,
   and opens the picker. `factory-<instance>` stops being a session name;
   `<role>-<instance>` now covers `gaffer-` and `worker-` only.
   *Done when:* `./factory` on a machine with two factories creates two tmux
   sessions, and the picker shows no reception row.

7. **Rewrite the charter's frame, not its rules.** Every hard line holds —
   no GitHub writes but the one RFC pull request, no Linear state, no dispatch,
   no merge. What changes is the top: a conversation the operator starts, in
   their workspace, that ends when they close it. Delete the sections on tmux
   naming, on boot and restart, and on being woken.
   *Done when:* the charter never refers to a session, a pane, or a fire that
   wakes it, and `contracts/what-is-a-factory.md` describes the desk the same
   way.

## Out of scope

Reception writing as its own identity (`identity/reception`) — unchanged by
this, still the durable fix for the GitHub rule. Multiple concurrent receptions
against one gaffer are permitted and uncoordinated: they share `notes.md` and
the inbox, both of which are append-mostly, and nothing about the desk was ever
exclusive.
