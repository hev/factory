---
name: init-factory
description: Set up a factory — the conversation that ends in factories/<name>.toml. Use on a fresh clone with nothing configured, when someone asks to add a second factory, or when they ask what a factory is for and the answer turns into standing one up.
---

# Setting up a factory

A factory is **one workspace and one job**. Its config is a dozen fields in
`factories/<name>.toml`, and `./factory init` writes them — you never edit that
file by hand and you never guess at a field name. Your part is the half a form
does badly: working out what this factory is *for*, and what belongs inside it.

Keep it to a handful of exchanges. Somebody who just cloned this wants a
factory, not an interview.

## First: check Linear, before you ask anything

Linear is where the operator approves RFCs, so a factory cannot boot without
it. Check it yourself rather than asking — three calls, and you only bring it
up if something is wrong.

```bash
claude mcp list          # which linear servers are registered, and are they up?
```

Then `get_workspace` through the MCP. Four ways this goes:

**More than one Linear server is registered.** Read `get_workspace` on each and
ask which workspace *this factory* is for, by name — never assume the one
called plain `linear` is the right one, and never assume the newest is either.
The tools arrive namespaced by server, so the answer becomes
`--linear-mcp-server <name>` and everything downstream uses it. Getting this
wrong points a factory at somebody else's board, and the first symptom is an
approval that never arrives.

**Connected, right workspace.** Say which one you found, move on.

**Connected to the wrong workspace.** The common case on a machine that
already uses Linear for something else, and it is fixable without disturbing
what is there. MCP OAuth sessions are keyed by *server name*, so a second
registration of the same URL holds a second workspace login:

```bash
claude mcp add --transport http --scope user linear-<instance> https://mcp.linear.app/mcp
```

Then they run `/mcp`, pick `linear-<instance>`, authenticate, and choose the
right workspace in the browser. Both logins now work side by side, and
`--linear-mcp-server linear-<instance>` goes in the config so this factory
uses theirs. Say plainly that the existing `linear` login is untouched — that
is the thing they are worried about when you ask them to re-auth.

**Not registered at all.** Same `claude mcp add` with the plain name `linear`,
then `/mcp` to authenticate. One minute, and it is the only prerequisite this
rig has that is not `brew install`.

**Do not carry on without it.** A config that names no team writes fine and
then no approval ever reaches the gaffer, which looks like a broken factory
rather than a missing login. Stop here and get it connected.

## Ask, in this order

**What is this factory for?** One job, in a sentence. The name falls out of the
answer — `acme`, `docs`, `search` — lowercase, dashes, no spaces. If they lead
with the name instead, ask what it is for anyway; the answer decides the scope.

**Which repos?** This becomes `repo_scope`, the only boundary in the system: a
gaffer works inside it and leaves everything else unread. Most repos are
already on the machine, so this is a scoping question rather than a fetching
one. Two things worth saying out loud if they come up:

- A factory scoped at somebody's whole workspace has no scope at all. Push back
  on a list that is really "everything I own".
- Adding a repo later is one line in the config. It is cheap to start narrow.

`ls ~/workspace` to see what is already there. If they name something that is
not on the machine, `gh repo clone <owner>/<name> ~/workspace/<name>` — that is
the only fetching you ever do, and never a fork, never a repo you create,
never over a tree they already have.

**Where do approved RFCs land?** `plans_repo` — the repo whose `plans/active/`
the gaffer commits approved RFCs into and then watches. Usually the main repo
in scope. It is added to `repo_scope` automatically.

**Which Linear team?** `list_teams`, and if there is exactly one, say which one
you are using rather than asking. One factory, one team: the team is the scope
wall in Linear the way `repo_scope` is the wall on GitHub.

**Which state means approved?** This is the one that matters, and it is the
one field nothing can guess. `list_issue_statuses team=<team>` and read the
list back to them — "which of these do you move something into when you mean
*build it*?" Most teams already have one, usually the first `unstarted` state
(`Todo`, `Ready to start`, `Up next`).

**The factory cannot create it.** Linear's MCP creates labels and not workflow
states, which is exactly why approval lives on a state — it is the one signal
the machine could not have minted for itself. If nothing on the list fits,
they add a state in Linear's team settings and you re-read the list. Do not
settle for a state that means something else; it is the door every piece of
work comes through.

On a default Linear workflow the answer is usually `Todo`, because `Backlog`
already means captured-but-undecided and `Todo` means decided-not-started —
which is exactly the line a factory needs.

The four labels (`rfc`, `blocked`, `testing`, `backlog`) need no question. The
gaffer creates them on its first tending beat.

**Where should it post?** `slack_webhook` — where the factory talks: a line
each time it dispatches a worker, and the WAITING ON YOU block whenever that
changes. Ask every time. A factory you never hear from is one you have to go
and look at, which is the thing this rig exists to stop, and the dispatch line
is what turns approving a plan into something you get an answer to.

**Ask for one thing: an incoming webhook URL.** If they do not have one, this
is the whole of it, and it is worth walking them through because it takes a
minute:

> api.slack.com/apps → Create New App → From scratch → pick the workspace →
> Incoming Webhooks → toggle on → Add New Webhook to Workspace → choose the
> channel → copy the URL.

Slack binds that URL to the channel they chose, so there is no channel id to
look up, no scope to pick, and no bot to invite — the three things that go
wrong. Paste it into `--slack-webhook` and the factory can talk.

**The URL goes to the login keychain, not into the config.** `factory init`
stores it under service `hev-factory`, account `<instance>/SLACK_WEBHOOK_URL`,
and writes a comment in the TOML saying where it went. Say so when you show
them the rendered config, because a webhook they cannot find in the file they
just made is the thing they will come back and ask about. On a machine with no
keychain, init says so and names the fallback:
`SLACK_WEBHOOK_URL_<INSTANCE>` in `~/.factory/secrets`.

"Not yet" is a real answer. Leave it out and `notify.sh` exits quietly every
beat, which is a normal factory and not a broken one; adding it later is one
line in the config.

**One channel per factory.** The webhook belongs to this instance, the same way
its repo scope does — a factory is one job, and its channel is where that job
reports. When you stand a second factory up, it gets its own webhook pointed at
its own channel; pointing both at one is two jobs interleaved in one feed.

**Only if they say webhooks are blocked, or they already run a Slack app:**
there is a bot-token path — `slack_channel` (the channel id, from Copy link,
and not a secret) plus `SLACK_BOT_TOKEN` in the keychain or
`~/.factory/secrets`, scoped `chat:write`,
bot invited to the channel. Do not offer it first. It is four more steps for
the same one line of output.

**Anything else, only if they ask.** `--runtime` is `resident` by default (a
claude session in tmux that schedules its own beat); `one-shot` moves the loop
out to launchd. The default is worth accepting, so do not walk somebody
through it unprompted.

## Then write it

Render it first and read it back:

```bash
./factory init --dry-run --name acme --plans-repo acme/api \
  --repo acme/api --repo acme/docs \
  --linear-team ENG --linear-approved-state "Ready to start" \
  --linear-review-state "In Review" --linear-backlog-state Backlog \
  --slack-webhook https://hooks.slack.com/services/T0.../B0.../...
```

Add `--linear-mcp-server linear-acme` when you registered a second server
above.

Show them the config. This is the moment a wrong scope or a wrong plans repo is
cheap to fix — after the first approval it is not. If it looks right, run the same
command without `--dry-run`.

Every failure is one line on stderr saying what was wrong. Fix it and re-run;
do not work around it by writing the TOML yourself.

## Then hand off

Say what happens next, plainly:

> Nothing gets built until you approve an RFC. Ask me for one — you bring
> something half-formed, I push back until it is specific enough that an agent
> cannot fill the gaps with a guess, then I write it up as an issue on your
> `<linear_team>` team. You approve it by moving it to
> `<linear_approved_state>`, and that is the only thing that counts — a
> comment is how you change it, not how you approve it, so "looks good but
> drop step 3" gets applied and then waits for you to move it. The gaffer's
> first beat after a fresh boot reports what it found and dispatches nothing,
> so the beat after that one is the first that builds anything.

Then stop and wait. Do not start drafting the RFC unprompted — they may want to
look around the picker first.

## Standing up a second factory

Same conversation, same command. The only difference is that there is a fleet
to compare against: if the new scope overlaps an existing factory's
`repo_scope`, say so before writing. Two gaffers against one repo is two agents
doing the same task with one of them wasted.
