---
name: slack-updates
description: How a factory talks in Slack — the WAITING ON YOU block, dispatch lines, and anything else that goes outward through scripts/notify.sh. Load before composing any outward post. Carries the link rule (every Linear issue and pull request clickable), the show-don't-tell evidence rule, and the length and voice a channel actually tolerates.
---

# Slack, from inside a factory

Slack is **the feed, not the gate**. Decisions live in Linear, code lives in
GitHub, and Slack is where a person finds out either one needs them — usually
on a phone, usually mid-something-else. Nothing is approved here and nothing is
recorded here that is not recorded somewhere durable first.

One outbound surface: `scripts/notify.sh <instance> [from] [--thread <key>]`,
message on stdin.
It spools every post before sending, so the front desk knows what you already
said. A factory with no Slack configured is a normal factory — `notify.sh`
exits quietly and the beat is unchanged. Never fail a beat over a status post.

One channel per factory. Sign every post with the instance — `— acme gaffer` —
because two factories in one channel is two jobs interleaved, and a post that
names itself is how that gets caught.

## What goes out

Exactly three things (`contracts/factory-loop.md`, steps 3 and 9):

1. **A dispatch line**, one per worker, the moment it starts.
2. **The WAITING ON YOU block plus IN FLIGHT**, when something changed. No
   change, no post — silence is the signal that nothing moved.
3. **A blocker or health failure** from `floor-watch.sh`.

Everything else is the spool's job. A post nobody can act on and nobody
learns anything from is a notification you spent for free.

## Work backwards

Lead with what changed for the person, not what the factory did. `deploy
pipeline refactored` is a diary entry; `signed-out visitors see prices again`
is news. The mechanism goes after the outcome, in the clause the reader can
skip.

The same for an ask: the first line is the decision you need, not the history
that produced it. Whoever wants the history taps through to the issue.

## Show, don't tell

- **Every user-visible change ships with a look at it.** Post the preview URL,
  deep-linked to the exact screen or state. Link previews are off and media
  unfurls are on, so a **page** URL stays one quiet line and an **image** URL
  renders as the picture — post the image's own URL when you want it seen, and
  say in the same line what it shows.
  Webhooks cannot upload files; a picture reaches Slack as a URL or not at all.
- **Cite checks, don't narrate them.** `CI green ‹run url›` beats a sentence
  about having verified things. Numbers over adjectives, always: "4 of 7 steps
  merged, 2 workers live" beats "good progress".
- **Nothing to look at is one clause, not a paragraph.** A migration or a build
  fix names its evidence — the log line, the passing job — and moves on.

## One thread per RFC

Pass `--thread <the RFC's Linear identifier>` on every message about one RFC —
dispatch, ready-for-testing, blocked, close. A build that can thread hangs them
all off that RFC's own conversation, so a channel running six plans is six
followable stories instead of one interleaved feed. This build posts flat and
ignores the key. **You pass it either way and never branch on which build you
are** — the capability is `notify/send`'s, not yours (`contracts/extending.md`
§3).

Pass **no** key for anything spanning several RFCs: the WAITING ON YOU block,
health failures, anything about the factory itself. A digest buried in one
issue's thread is a digest nobody reads, and that block is the one post that
has to land in the channel.

The thread is not a log. It carries the same three kinds of message it always
did — the fact that they are collected is the whole benefit, and it is not a
licence to post progress into them.

## Link everything, both ways

**Every Linear issue and every pull request is a full clickable URL.** A bare
`#12` or `HEV-31` is a posting defect: on a phone it is a dead end, and a dead
end is where an approval stops. Give both when both exist — the issue is where
they decide, the pull request is where they merge — and put the preview link
ahead of either when there is a screen to look at.

Order inside a line: **outcome → preview → issue → pull request → check.** The
reader stops as soon as they have what they need, so what they need goes first.

Backlink too: whatever you post here about an issue should already be readable
on that issue. Slack is where they hear it; Linear is where they act on it.

## How it reads

Short and with a pulse. The channel is one job's report, and a wall of text is
how a channel gets muted.

- **One to three lines per item.** The block is scannable in a glance or it has
  failed. No preamble, no sign-off beyond the instance name, no restating the
  ask you posted an hour ago.
- **Witty is allowed; cute is not.** One dry clause per post, on the facts as
  they are — a build that fell over can say so with a straight face and a
  little edge. The voice never obscures a number, never replaces a link, and
  never adds a line. If the joke costs a line, the joke goes.
- **Never the same joke twice.** A recurring bit in a status feed is a laugh
  track.
- **No emoji-as-heading, no bold labels, no horizontal rules.** A `⏭` on the
  top item and a state marker are the whole visual vocabulary.
- **Facts survive the edit.** Cut adjectives and reasoning; keep every link,
  gate, state, and number.

The test before every send: **can they act on this, or learn something from it,
from a phone, without opening anything else?** If not, it belongs in the spool.

## Shape of the two regular posts

A dispatch line — the outcome, then where it is tracked:

```
▶ acme dispatched: sitemap regenerates on publish — https://linear.app/…/HEV-31
```

The block leads with **Ready for Testing**, then **Blocked**, then `IN FLIGHT`
and `NEXT`. Every item: what it is, which gate, and the links, top item marked
`⏭`. "WAITING ON YOU: nothing" is said out loud rather than left implied. The
full composition rules — freshness, review-before-listing, ordering by
leverage, the two-report limit before parking — are step 8 of
`contracts/factory-loop.md`; this file governs how it reads once composed.
