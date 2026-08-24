# hev factory

Stop prompting Claude directly.

hev factory orchestrates a crew of coding agents on a Mac you own: a front desk
that takes the work, a loop that breaks an approved RFC into tasks, and a
worker per task in its own session. It is for work that takes longer than one
session and spans more than one repo — you approve a plan, and it reports what
needs you.

> ## ⚠️ Read this before you run it
>
> **A factory runs in yolo mode, as you, on your machine.**
>
> A one-shot beat runs `claude --permission-mode bypassPermissions`
> ([`factory-iterate.sh`](factory-iterate.sh)), and the workers it dispatches
> are `claude` sessions started by an agent, not by you. They run shell
> commands, write files, commit, push, and open pull requests with your `gh`
> login, against whatever the machine can reach.
>
> If you connect Linear it writes there as you as well: it moves issues through
> your team's workflow, comments, and labels. The one thing it never does is
> perform your approval — it never moves an issue into the state that means
> approved, and it never merges a plan into your plans branch, because that is
> how it is told to build something.

## Quick start

### 1. Install

```bash
brew install hev/tap/factory
gh auth login
```

The formula brings `tmux`, `gh` and `jq` with it. macOS only — this is launchd,
tmux and the login keychain, so there is no Linux build to fall back to.

You also need `claude`, logged in. The factory ships no model and never sees
your API keys: reception, the loop, and every worker it dispatches run `claude`
on the subscription you already hold. Every agent is a tmux session, which is
why you can attach to any of them mid-task and take over by typing. Run it on a
Mac that never sleeps — a laptop that sleeps stops the loop mid-beat, and a Mac
mini is the intended shape.

### 2. Run the factory

```bash
factory
```

The first run has no checkout to read, so it asks before cloning this repo to
`~/workspace/factory` and starts from there. The contracts a factory runs on
live in that checkout and are yours: changing how a factory operates is a commit
rather than a setting, which is only true of a tree you own. It sits *alongside*
your work and never inside it — this is generic machinery, and your own repos
(and Linear team, if you connect one) stay the source of truth for plans,
issues, and pull requests.

There is nothing to configure by hand. Reception goes on duty in a tmux session,
you land in the conversation, and it walks you through your first factory from
there.

**Approving is the one thing that is yours**, and there are two ways to do it.
Out of the box it is **merging a pull request** that adds the plan to
`plans/active/` — nothing in the factory merges into that branch, which is
exactly why the merge means something. Unprotected, that is a rule the factory
follows; protect the branch and it becomes one it cannot break, at the cost of
having to merge its bookkeeping commits too
([`approvals.md`](contracts/approvals.md) has the trade).

**Linear is better and setup offers it**, because moving an issue into your
team's *approved* state is one tap from a phone, and approving on the
way somewhere beats approving next time you are at a machine. It costs one
OAuth:

```bash
claude mcp add --transport http --scope user linear https://mcp.linear.app/mcp
```

Then `/mcp` in any `claude` session to authenticate. Your team needs a workflow
state that means *approved*, which on a default board is `Todo`: `Backlog`
already means captured-but-undecided, and `Todo` means decided-not-started.
Setup asks which one you mean and never guesses. Skip it now and add it later —
it is two fields in the config.

If you already use Linear for something else and this factory is for a
different workspace, register a second server under its own name —
`linear-acme`, same URL — and authenticate that one against the other
workspace. MCP logins are keyed by server name, so both work side by side and
the config names which one this factory uses.

### 3. In another pane, watch the floor

```bash
factory
```

That is the picker, and it is the screen you leave open: reception, the gaffers,
and every worker they dispatched. `↵` on any row attaches to that session, so
watching becomes steering the moment you start typing.

## From source

Working on the factory itself, or running an unreleased revision:

```bash
git clone https://github.com/hev/factory ~/workspace/factory
cd ~/workspace/factory
./factory list
```

This one needs Go. `./factory` builds the picker and copies it into your
`$GOBIN` so `factory` works from any directory; on a fresh clone it reports
nothing configured, which is right. Set `FACTORY_NO_GLOBAL_INSTALL=1` to leave
your `$PATH` alone.

## Where the rest of it is

**[hevfactory.com](https://hevfactory.com)** is the story: what reception,
RFCs and loops actually are, how approving works, what the picker puts
on screen, and how to run the machine on a timer. Start there if you are
deciding whether this is for you.

**[`contracts/`](contracts/)** is the machine itself. Every file in that
directory changes what a factory does when you edit it — the gaffer and the desk
read them by path, mid-iteration — and nothing else in this repo has that
property. [`what-is-a-factory.md`](contracts/what-is-a-factory.md) is the
normative definition the rest should be explainable in terms of, and
[`factory-loop.md`](contracts/factory-loop.md) is one iteration written out in
prose. Changing how a factory operates is a commit, not a setting.

**[`contracts/extending.md`](contracts/extending.md)** is the one page for the
four places a factory calls out to something it does not ship: where a beat
runs, which account it acts as, how it reaches you, and where credentials live.
Each is a file that either exists or does not — no registry, no plugin API.

Everything else here is ordinary: [`docs/picker.md`](docs/picker.md) documents
the screen, [`factories/example.toml`](factories/example.toml) documents every
config field, and [`plans/README.md`](plans/README.md) is the house style for an
RFC, including the 120-line budget.
