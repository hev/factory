---
name: slack-updates
description: How a factory talks in Slack — the house style for anything that goes outward through scripts/notify.sh. This build's loop and workers post nothing; a foreman does, on a build that has one (contracts/extending.md §6). Load before composing any outward post. Carries the link rule (every Linear issue and pull request clickable), the show-don't-tell evidence rule, and the length and voice a channel actually tolerates.
---

# Slack, from a factory

Slack is **the feed, not the gate**. Decisions live in Linear, code lives in
GitHub, and Slack is where a person finds out either one needs them — usually
on a phone, usually mid-something-else. Nothing is approved here and nothing is
recorded here that is not recorded somewhere durable first.

One outbound surface: `scripts/notify.sh <instance> <from> [--thread <key>]`,
message on stdin. It spools every post before sending, so the machine's record
says what the operator has already been told. A factory with no Slack
configured is a normal factory — `notify.sh` exits quietly.

## Who speaks

**One voice.** The loop does not post (`contracts/factory-loop.md`, step 9)
and workers never did: a beat writes its report, and the floor writes the
spool. What goes outward is a **foreman's digest** — one post per channel per
run, covering every factory that reports there — on a build that has one
(`contracts/extending.md` §6). This build has none and posts nothing.

Several agents each narrating their own slice into one channel is the
arrangement this replaces: a dispatch line here, a block there, a worker's
progress in a thread, three factories interleaved. Every one of those posts
was true, and together they were unreadable. A digest is coordinated because
one thing wrote it.

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
- **Cite checks, don't narrate them.** `CI green ‹run url›` beats a sentence
  about having verified things. Numbers over adjectives, always: "4 of 7 steps
  merged, 2 workers live" beats "good progress".
- **Nothing to look at is one clause, not a paragraph.** A migration or a build
  fix names its evidence — the log line, the passing job — and moves on.

## Threads

`--thread <key>` names the conversation a message belongs to, normally the
Linear identifier of one RFC. A digest spans every RFC and passes **no** key:
it belongs in the channel, where somebody sees it, and a digest buried in one
issue's thread is a digest nobody reads. A build that can thread may hang a
message about one RFC off that RFC's conversation; this build posts flat and
ignores the key. Pass it or not by what the message is about, never by which
build you are (`contracts/extending.md` §3).

## Link everything, both ways

**Every Linear issue and every pull request is a full clickable URL.** A bare
`#12` or `HEV-31` is a posting defect: on a phone it is a dead end, and a dead
end is where an approval stops. Give both when both exist — the issue is where
they decide, the pull request is where they merge — and put the preview link
ahead of either when there is a screen to look at.

Order inside a line: **outcome → preview → issue → pull request → check.** The
reader stops as soon as they have what they need, so what they need goes first.

Backlink too: whatever is posted here about an issue should already be readable
on that issue. Slack is where they hear it; Linear is where they act on it.

## How it reads

Short and with a pulse. The channel is the machine's report, and a wall of
text is how a channel gets muted.

- **One to three lines per item.** The post is scannable in a glance or it has
  failed. No preamble, no sign-off beyond the speaker's name, no restating an
  ask posted an hour ago.
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
from a phone, without opening anything else?** If not, it stays in the record.

## Shape of a digest

It leads with **WAITING ON YOU** across every factory in the channel —
**Ready for Testing** first, then **Blocked** — every item with what it is,
which gate, and the links, top item marked `⏭`, and "WAITING ON YOU: nothing"
said out loud rather than left implied. Then one line per factory: last beat,
what is in flight, what it cost. Then whatever the machine's records and the
gaffers' reports disagree about — a late beat, a failed iteration, a worker
grinding with nothing to show — which is the part only a supervisor can write.
Signed by the speaker: "— foreman".
