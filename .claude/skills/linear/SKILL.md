---
name: linear
description: How a factory uses Linear — the operator's surface for RFCs, approvals, asks, and queues. Load before reading approval state, filing an ask, labelling a queue, or writing anything into Linear at all. Carries the state/label boundary, the user-story shape of an RFC, the screenshot-and-preview-link posting style, and the MCP calls that do each job.
---

# Linear, from inside a factory

Linear is **the operator's surface, and the whole of it**. RFCs, approvals,
asks, blockers, the backlog — all of it lands here, and a person acts on it
from a phone. GitHub holds branches, pull requests, and CI, none of which
waits on anybody.

Which workspace, which team, and which states mean what are in
`factories/<instance>.toml`: `linear_team`, `linear_approved_state`, the
optional `linear_review_state` and `linear_backlog_state`, and
`linear_mcp_server` when the machine holds more than one Linear login. Read
them before your first call. Never guess a team name from the repo name, and
never guess a state name from this file — every team words its own workflow.

When more than one Linear server is registered the tools arrive namespaced by
server name. Use the one the config names and no other — the second login is
somebody else's workspace, or the operator's own view of this one, and either
will answer your calls just as readily. A factory posting through the
operator's login is a factory whose comments are indistinguishable from theirs,
which is the confusion a human gate cannot afford.

## The one boundary

**You never move an issue into `linear_approved_state`.**

That is the whole rule, and it is the only state transition you are barred
from. Approval is that state and nothing else — no label, no comment, no
phrase. The operator moves the issue, and that transition is the entire
signal.

Every other transition is yours, and you are expected to make them: an
approved RFC to in-progress when workers start, to the review state when it is
verified and waiting on somebody, to done when the plan archives. Moving an
issue through the team's workflow is how the board stays true.

This is not a promise you are making about the *whole* of state. Linear's MCP
can create labels (`create_issue_label`) and cannot create workflow states, so
the marker carrying the decision is one you had to be handed rather than one
you could mint. Moving an issue into it anyway is a factory defect of the same
severity as dispatching against an unapproved plan.

**Read the actor before you act on an approval.** Where `linear_mcp_server` is
authenticated as the factory's own account rather than the operator's, every
transition carries who made it, and an issue that arrived in
`linear_approved_state` by the factory's own hand is not approved — it is the
defect above, already committed. Check it on the transition you are about to
dispatch against, not on a sweep: an approval you cannot attribute to a person
is one you do not act on, and the discrepancy goes in the status report with
the issue URL. On a workspace where the factory and the operator share one
login there is nothing to read, and the rule stays what it always was, a
promise. That is the difference the separate account buys, and the only reason
it is worth a seat.

## Reading

Cheap reads, in the order you need them.

```
list_issues  team=<linear_team> label=rfc state=<linear_approved_state>
             fields=["title","url","description","status","updatedAt"]
```

`fields` is not optional politeness — the default payload carries everything,
and a beat that pulls full issue bodies it will not read is spend with no
answer attached. Ask for the columns you are about to use.

`state` takes a state **type** as readily as a name, so `state="started"`
covers a team's custom in-progress states without knowing what they are
called. Name the state only where the exact one matters, which is approval.

`updatedAt="-PT30M"` bounds a sweep to what moved since the last beat.
`list_comments issueId=<id>` reads a thread. `get_issue` is for one issue you
already have an identifier for, never for a scan.

## Writing

Three gotchas, each of which has eaten somebody's data:

- **`labels` on `save_issue` replaces the whole set.** Passing `["blocked"]`
  removes every label the operator put there. Read the current labels, add
  yours, write the union — always.
- **`save_comment` with `id` edits in place.** This is how an ask stays one
  comment instead of four. Keep the id; editing is the default and posting a
  second comment is the exception.
- **`patch` on `save_issue` edits a description surgically.** Applying a
  condition to an RFC is a `replace` op against the step it changes, not a
  rewrite of the document. A rewritten description loses the operator's own
  edits and shows up as a wall of diff in their inbox.

Markdown goes in literally — real newlines, not `\n`.

## What you post, and nothing else

Four things. The list is exhaustive; anything not on it is noise on a tracker
a person reads.

1. **An ask.** First line `ASK: <the one question>`, and the question is a
   decision, not a status. One comment per question, **edited** as the ask
   sharpens. A second comment means the question itself changed.
2. **Verified-ready.** The preview link first, then the pull request URL, what
   you checked, what they do next. Three lines.
3. **A close.** One line linking what shipped, when the issue's answer landed.
4. **A blocker's own reason**, when a worker died on something the operator
   has to clear.

Never post progress. Not "worker dispatched", not "picked this up", not
"still working". The floor reports through Slack and the event spool, and the
issue tracker is where somebody comes to find what needs *them*. A comment
they read and cannot act on is a notification you spent for nothing.

## Show, don't tell

A person deciding from a phone should not have to read your description of
what the screen looks like. Give them the screen.

- **Screenshot every user-visible change.** Take it against the deployed
  preview, not a local dev server, and post it on the comment that asks for
  the decision. Upload with `prepare_attachment_upload` →
  `create_attachment_from_upload`, then embed the returned URL as
  `![what it shows](<url>)` in the comment body. Before-and-after when the
  change is a modification; one image when it is new.
- **Lead with the preview link.** CI already builds one per pull request —
  the deploy preview, the Storybook route, the staging URL. That link is the
  approval surface; the pull request is the receipt. Deep-link straight to the
  screen or state under review, with whatever query params or seeded route it
  takes to land there, so the first tap lands on the thing and not a home page.
- **Cite the check, don't narrate it.** "CI green (<run url>), Lighthouse 98"
  beats a paragraph about what you verified. When a criterion was checked by a
  test, link the run; when it was checked by eye, post the screenshot.
- **No preview, say so.** A change with nothing to look at — a migration, a
  build fix — gets one line naming the evidence that stands in for the picture
  (the log line, the query result, the passing job). Never an apology and
  never a paragraph explaining why there is no screenshot.

Screenshots and links are what buy the brevity below. The picture carries the
detail so the text does not have to.

## How it reads

The first line carries the message. Linear shows roughly that much in the
inbox and in the push notification, and the rest is for whoever opens it.

- No headings, no bold labels, no emoji, no horizontal rules. A Linear comment
  is a sentence or two; a comment with a table of contents is a document that
  went to the wrong place.
- No preamble and no sign-off. Skip "Update:", "Quick note", "Here's where we
  landed", and the closing summary of what you just said. The issue already
  knows which factory it belongs to.
- Numbers over adjectives: "4 of 7 steps merged, 2 workers live" beats "good
  progress".
- Every reference is a real URL. A bare `#12` a reader cannot tap is a posting
  defect.
- Never restate the issue back at the operator. They wrote it.

**Length is a hard rule.** A comment is one to three lines plus its links and
images. Linear is a human gate, and every line you add is one a person reads
before they can decide. Cut the reasoning and keep the decision; if the
reasoning matters, it belongs in the pull request where whoever wants it will
go looking.

The test before every write: **can they act on this, from a phone, without
opening anything else?** If not, it belongs in the status report, the spool,
or nowhere.

## The RFC issue

**Assign it to `linear_assignee`** from `factories/<instance>.toml`, on the call
that creates it. Linear notifies an assignee and lists the issue in their own
inbox; an unassigned RFC waits somewhere nobody is looking, which is the whole
failure the board was meant to fix. Absent from the config, file it unassigned
rather than guessing a person — a guessed assignee sends somebody else's
decision to the wrong inbox, and they will approve it.

Assigning is not approving and never becomes approving. The assignee is a
field; approval is the move into `linear_approved_state`, and that stays the
operator's alone.

One Linear issue per RFC, labelled `rfc`, and the description **is** the plan
— the house style in `plans/README.md` applies unchanged: a work list under
120 lines, an acceptance condition on every step, no essay. The title is the
outcome in a phrase, with no `RFC:` prefix; the label says what it is.

Work backwards from the person, and say so in their words. An RFC opens on
three things, in this order, before any design:

1. **The user story.** `As a <who>, I want <what>, so that <why>.` One
   sentence. If it takes two, it is two stories or the wrong altitude.
2. **Acceptance criteria.** A short list, each one observable — what is true
   when this is done, phrased so the operator can look at the thing and say
   yes or no. Not "the endpoint is refactored"; "a signed-out visitor sees the
   pricing table without a flash of empty state".
3. **How to test it.** The steps *they* would take and the tools they would
   take them with — the URL to open, the button to press, the command to run,
   the account to be signed in as. A criterion with no way to check it is an
   opinion.

The design, the steps, and the constraints come after those three, and only
what a worker cannot guess. An RFC that opens on implementation goes back.

Sub-issues per step are not how this works. The plan document is the work
list, the child ledger is what is in flight, and a tracker that grows an issue
per dispatched worker is the machine filing tickets at a human.

## The markers

Four things need marking, and each is a state when the team has one for it and
a label when it does not. The config decides, per factory:

| Marker | Where it lives | Means |
|--------|----------------|-------|
| `rfc` | always a label | This issue is an RFC. Its description is the plan. |
| `blocked` | always a label | Needs a decision or a human step. Carries an `ASK:` comment. |
| ready for testing | `linear_review_state` if set, else a `testing` label | Factory-verified, one action from shipping. |
| parked | `linear_backlog_state` if set, else a `backlog` label | Captured, not in `plans/active/`. Never dispatched. |

`rfc` and `blocked` stay labels on every team. `rfc` marks what an issue *is*
rather than where it stands, and no default Linear workflow has a state for
"waiting on a human decision" — `blocked` has to sit alongside whatever state
the issue is in, which is what a label is for.

Create the labels you need idempotently with `create_issue_label`. An issue is
in at most one of the testing/parked positions at a time.

## When Linear is not reachable

Say so once, in the status report, and carry on. A beat that cannot read
Linear approves nothing and dispatches nothing new — which is the safe
direction — but it still tends live workers, still harvests, and still writes
its beat line. Silence is not a stall; report the reason.
