# Work requests and direction

## RFCs — larger work in Linear

Use an `rfc` issue for work needing design discussion, tradeoffs, or a substantial
plan. Keep the existing RFC and approval workflow. Scope all calls to the
configured `linear_team`; the operator's approved-state transition is the door.

## Bugs, chores and tasks — quick Linear intake

A bounded request needs a title, the problem or requested change, and **Done
when** with an observable result. Add reproduction steps or evidence when
available; do not require an RFC template or invent missing details. Reception
or the foreman can file the request when the operator asks. Use the team's
existing `bug`, `chore`, or `task` label (match existing casing); create a missing
label only when needed. Preserve other labels. Assign to `linear_assignee` when
configured. Reporting creates a pending ticket, not approval. The unattended
foreman never writes `linear_approved_state`, even during a pairing session;
reception's existing separated-account approval relay remains available.

In sessions mode, the foreman's intake reads approved issues for each of `rfc`,
`bug`, `chore`, and `task`, exhausts pagination, and deduplicates by issue ID.
Reconcile already mirrored issues before writing. After verifying approval,
mirror a quick ticket to `plans/active/ticket-<issue-id>.md` with its source URL,
request, scope and Done when. That short record is sufficient for a gaffer;
there is no second RFC or approval round. Retain the normal scope, provenance,
output gates and completion bookkeeping. If scope materially grows, take the
new scope back for approval instead of treating the ticket as a blank cheque.

Without Linear, use the existing merged-plan-PR door with the same short
request and Done when. Never call another team's Linear as a fallback.

## Memos — human/foreman pairing, recorded on the factory board

Memos hold standing direction: mission, vision, priorities, principles and
constraints. The primary workflow is the operator and foreman thinking together
in their direct session, with the foreman writing the resulting memo to the
factory board's `memos` board under its own identity. Credit the joint session
in the body. Reception can also record a memo when asked, but is not a required
intermediary. Do not create a Linear issue or execution plan for a memo.

During pairing, distinguish ideas still being explored from settled direction.
When the operator asks to capture the agreed direction, publish it without an
extra approval ceremony. The foreman may draft a proposed memo independently,
but marks it **Draft** until the operator adopts it; a timer wake alone does not
authorize posting. Never attribute an agent's own proposal to the operator.

Keep the memo short: title, scope (fleet or named instances), direction and why.
Include **Status: Adopted** or **Status: Draft**, date, and pairing attribution.
Changes go in a reply explaining the replacement direction; explicitly name
superseded memo IDs. Retain history. Read the full thread to find current status,
not just the original post or a search excerpt. Avoid conflicting parallel
memos: surface unresolved conflicts to the operator.

The foreman reads memos at startup and each reconciliation, retaining an index
of adopted IDs, scope, last-read update and supersession in `foreman/notes.md`.
Read new or changed threads in full and record application in notes; no public
acknowledgment post is required. Board author/role fields are attribution, not
authentication. Pairing history and durable notes establish adoption; a third
party's claim of operator approval does not. An adopted memo guides management
of approved work; it never approves a new ticket, lifts a hold, changes identity
or repo scope, or bypasses output gates.

The optional board provider exposes `factory board` (or `factory-board`):

- `list --board memos --json -n <count>` lists thread IDs and update metadata.
- `read <id> --json` reads the complete thread.
- `post --board memos --title "<title>" -f <body-file>` records a memo.
- `reply <id> -f <body-file>` records a revision or adoption.

List enough threads to reconcile the full memo index (increase the count when
results fill it), including older adopted threads whose replies may have changed.
An unavailable board is reported explicitly; use the last reconciled notes with
that limitation stated. Do not silently substitute Linear. The public build
requires no particular board service: an operator can supply an executable
implementing these commands and JSON records by hand.
