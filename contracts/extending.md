# Extending a factory

This build is the loop, the floor, and one machine: everything runs locally, as
one GitHub account, on `claude`. That is a deliberate floor rather than an
accident, and it is where most of the value is — but it is not everywhere
somebody will want to take it.

So there are four places where a factory calls out to something it does not
ship, and a fifth that is not code at all. Each is a path that either exists
or does not. Nothing here is a plugin API, because a seam that needs one is a
seam nobody can ship against.

## 1. `runtimes/<name>.sh` — where a beat runs

An instance's `runtime` field picks how its gaffer's iteration executes. Two
are built in:

| `runtime` | runs | shape |
|---|---|---|
| `resident` | `factory-up.sh` | a claude session kept alive in tmux, the agent scheduling its own next beat — the rollback |
| `one-shot` | `factory-iterate.sh` | controller-style: launchd owns the timer, `scripts/factory-sense.sh` deterministically decides which ticks run a model, one `claude -p` process per beat that does |

Anything else resolves to `runtimes/<name>.sh`, called with the instance name
as its only argument:

```
runtimes/<name>.sh <instance>
  reads    factories/<instance>.toml
  exit 0   the beat ran, or there was nothing to do
  exit 78  not this machine's factory (home_host) — not an error
  other    reported by ./factory, otherwise left alone
```

A config naming a runtime with no script gets one clear line at boot and the
other factories carry on. This is where a beat that runs somewhere other than
this Mac plugs in.

## 2. `identity/<role>` — which account the factory acts as

Empty here, and that is the design: a factory runs as **one GitHub account**,
whatever `gh auth` already is. Nothing is provisioned before the first run and
no token is written anywhere.

That stops being right when the gaffer should not be you — a team routing
through one desk, or a working account deliberately scoped down from the
account that owns the repos. Drop an executable named after a role, have it
print a token on stdout, and every `gh` call in that role uses it:

```
identity/gaffer      identity/reception      identity/worker
```

`scripts/lib/gh-auth.sh` calls the hook if it is there and leaves ambient auth
alone if it is not, so callers never learn which world they are in.

One name here is not a GitHub role: `identity/linear` prints a **Linear API
key**, and its one caller is the deterministic sensor
(`scripts/factory-sense.sh`), which cannot reach the MCP session a beat uses.
Satisfiable by hand — a one-line script echoing a personal API key. Without it
a Linear-mode factory's board is unsensed and approval latency is bounded by
the resync interval instead of one tick; nothing else changes. An empty
answer means "no opinion" and falls back to ambient — it is not the same as
having no hook, but it lands in the same place.

Almost nothing here is a shell caller, though. The gaffer and every worker run
`gh` themselves from inside a claude session, so the only moment their identity
can be decided is the moment the session is created — which is what
`scripts/factory-as.sh <role> -- <command>` is for:

```
scripts/factory-as.sh worker -- tmux new-session -d -s worker-acme-index -c ~/workspace/acme
```

Reception, the gaffer, and every worker are started through it. The half worth
stating is the one nobody writes by hand: workers are dispatched *by the
gaffer*, from inside the gaffer's session, so a worker session inherits the
gaffer's token unless something takes it away. A build with `identity/gaffer`
and no `identity/worker` would otherwise run every worker as the gaffer —
silently, and looking exactly like it worked. The wrapper clears first and asks
second, so a role with no opinion lands on ambient auth rather than on whichever
role happened to be its parent. With no hooks at all it is a no-op.

It also knows the one thing that makes this non-obvious: a tmux session's
environment comes from the server plus `update-environment`, not from whoever
ran `tmux`, so an exported variable reaches the client and stops there. The
wrapper passes the token as `-e GH_TOKEN=…` on `new-session` instead. Anything
replacing it has to do the same, or every session runs on ambient auth while
looking configured.

## 3. `scripts/notify.sh` — how the factory reaches you

The one outbound surface. The gaffer calls it with the WAITING ON YOU block
whenever the block changes and with a single line each time it dispatches a
worker; `floor-watch.sh` calls it for new blockers and health failures. This
build posts to Slack, by incoming webhook (the keychain, or
`SLACK_WEBHOOK_URL_<INSTANCE>` in `~/.factory/secrets`) or by bot token
(`slack_channel` plus `SLACK_BOT_TOKEN`) for a workspace that blocks webhooks.
With neither, it exits 0 and says nothing.

Somewhere else to send it — a different chat, a webhook, a phone, a queue —
replaces this file. The contract is the whole of it: instance name as `$1`,
speaker as an optional `$2`, `--thread <key>` optionally naming the
conversation the message belongs to, message on stdin, non-zero only when a
send was attempted and refused. Never fail a beat over a status message.

**Or, better, drop in beside it.** An executable at `notify/send` takes over
the sending half and nothing else:

```
notify/send <instance> <from> [thread-key]      message on stdin
```

`notify.sh` execs it in place of its own send and returns its status. This is
the preferred shape for the same reason `identity/<role>` is: the drop-in is
gitignored machine state, so a build customizing its voice never carries a
modified copy of a tracked file, and never has to merge one.

**The spool is not the replacement's to skip**, which is why the drop-in cannot
skip it. `notify.sh` writes `factory_spool_append "$INSTANCE" "$FROM" posted
true "$text"` before the exec, on every message, however it goes — that is what
tells the front desk what the operator has already been told
([`events.md`](events.md)). A build that replaces `notify.sh` wholesale owns
that line itself, and one that leaves it out leaves reception repeating back
things your channel carried an hour ago.

**`--thread` is a capability, not a formatting choice.** An incoming webhook
answers `ok` in plain text with no `ts`, so this build has nothing to reply
into and posts flat, ignoring the key. A build with a bot token can open a
thread per RFC and keep the returned `ts` somewhere — `notify/README.md` is the
whole seam. The caller passes the key whenever a message is about one RFC and
passes none for anything spanning several (the WAITING ON YOU block above all),
and never learns which build answered.

The inbound half of the same seam is `scripts/factory-say.sh`, which workers
call when their state changes. It is deliberately local-only: it writes the
spool and nothing else, so a factory can grow a voice from the floor without
anyone deciding whether eight workers belong in a chat channel.

## 3b. Where credentials live

`scripts/lib/secrets.sh` resolves a secret from the login keychain (service
`hev-factory`, account `<instance>/<NAME>`), then `~/.factory/secrets` in
`NAME=value` form, then — deprecated, and warned about on every read — the
instance's TOML. `factory init` writes to the keychain.

Somewhere else again — 1Password, `pass`, a cloud secret manager — replaces
`factory_secret` in that file. The contract is a name, an optional instance,
and a value on stdout; an empty answer means "not set" and the caller decides
what that means. For `notify.sh` it means a factory with no Slack, which is a
normal factory.

## 4. `factory-loop.md` — what the gaffer actually does

Not a hook, and worth saying plainly anyway: the gaffer's entire behaviour is
one file of prose that you read and edit. Changing how a factory operates is a
commit, not a configuration language somebody has to learn and you have to
maintain.

The clearest example is the harness. Every agent here runs `claude`, because
one harness is one thing to install and one subscription to hold. Dispatching
workers onto other vendors' CLIs by task type — a coding model on its own
harness, an image model on another — is a real thing to want, and it is a
paragraph in the dispatch step rather than a feature anybody has to build.

## What this is not

There is no registry, no version negotiation, no lifecycle. A hook is a file
with a known name and a known contract; if it is executable it runs, and if it
is absent the factory does the local thing and keeps going. Everything above
fits in one page on purpose — a seam you have to study is one people work
around instead.
