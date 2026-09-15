> Approved source: https://linear.app/hevmind/issue/FAC-37/intake-adopts-a-new-issue-onto-an-unrelated-gaffer-whenever-an

## Problem

`scripts/factory-controller.py` `intake()` has a fallback for legacy assignments that predate the `issue` field:

```python
prior = next((r for r in s.records() if r['status'] != 'retired' and
              Path(r['plan']).exists() and issue['url'] in Path(r['plan']).read_text()), None)
if prior:
    prior['issue'] = ident
    s.write(STATE / 'gaffers' / (prior['session'] + '.json'), prior)
    continue
```

A substring match on the plan text is treated as proof of ownership. But a plan **is** the issue description, and Linear's MCP rewrites every issue mention in a description into `<issue id="…" href="https://linear.app/…/FAC-nn/…">FAC-nn</issue>`. So any plan that merely *refers* to another issue contains that issue's URL, and the next time that issue is approved intake silently hands it to the unrelated gaffer and `continue`s — no new assignment, no problem recorded, and the existing record's `issue` field is overwritten.

Two further faults in the same three lines:

* **It is silent.** The approved issue simply never gets a gaffer, and `poll()` reports `problems: []`.
* `s.records()` **is not instance-scoped.** A plan in one instance can adopt another instance's issue, which crosses the `linear_team` / `repo_scope` wall that "one factory, one team" exists to enforce.

## Evidence

2026-09-15. <issue id="8ae9c526-f3d2-4383-9c8b-a2236ee610cf" href="https://linear.app/hevmind/issue/FAC-33/the-floor-digest-wakes-gaffers-on-file-mtime-so-a-waiting-ci-record">FAC-33</issue> was moved to Todo at 01:30:44Z and an attended receipt was recorded. Two consecutive polls returned `problems: []` and created no gaffer; `plans/active/ticket-fac-33.md` was never written. A read-only probe confirmed the controller's own Linear client did see it:

```
approved count: 1
  FAC-33 | ['Bug'] | The floor digest wakes gaffers on file mtime, so a
```

The cause: <issue id="cc9b7dd4-e82d-44d2-b74a-3afdb1a4d631" href="https://linear.app/hevmind/issue/FAC-35/run-turn-drops-a-turn-silently-when-no-global-slot-is-free-so-a">FAC-35</issue>'s description cites <issue id="8ae9c526-f3d2-4383-9c8b-a2236ee610cf" href="https://linear.app/hevmind/issue/FAC-33/the-floor-digest-wakes-gaffers-on-file-mtime-so-a-waiting-ci-record">FAC-33</issue> as related, so `plans/active/ticket-fac-35.md` contained the <issue id="8ae9c526-f3d2-4383-9c8b-a2236ee610cf" href="https://linear.app/hevmind/issue/FAC-33/the-floor-digest-wakes-gaffers-on-file-mtime-so-a-waiting-ci-record">FAC-33</issue> URL twice. Intake adopted <issue id="8ae9c526-f3d2-4383-9c8b-a2236ee610cf" href="https://linear.app/hevmind/issue/FAC-33/the-floor-digest-wakes-gaffers-on-file-mtime-so-a-waiting-ci-record">FAC-33</issue> onto `gaffer-factory-ticket-fac-35` and rewrote that record:

```
gaffer-factory-ticket-fac-35.json   issue=FAC-33   plan=…/ticket-fac-35.md   source=…/FAC-35/…
```

An assignment whose `issue`, `plan` and `source` disagree will take `linear-update` events for the wrong issue and report against it.

Recovered by hand: the record's `issue` was restored to <issue id="cc9b7dd4-e82d-44d2-b74a-3afdb1a4d631" href="https://linear.app/hevmind/issue/FAC-35/run-turn-drops-a-turn-silently-when-no-global-slot-is-free-so-a">FAC-35</issue> (backup `/tmp/fac35-record.bak.json`), and the `<issue …>` markup in `plans/active/ticket-fac-35.md` was rewritten to the bare identifier so the substring no longer matches (backup `/tmp/ticket-fac-35.md.bak`). That local plan now differs cosmetically from its Linear description; the description is unchanged and remains canonical. The next poll then assigned `gaffer-factory-ticket-fac-33` correctly.

**This is armed across the board, not a one-off.** Foreign issue URLs currently sitting in `plans/active/`:

```
factory-inference-cost-attribution.md: FAC-11 FAC-13 FAC-18 FAC-9
hev-loop-crd.md:                       FAC-20 FAC-3
kit-dashboard-fast-load-and-preview.md: FAC-1 FAC-11 FAC-20 FAC-9
mini-hard-restart-recovery.md:         FAC-13 FAC-2 FAC-22
mini-is-one-repo.md:                   FAC-2 FAC-20
preview-screenshot-verification.md:    FAC-14
ticket-fac-33.md:                      FAC-27 FAC-28 LYR-51 LYR-52
worker-shared-build-caches.md:         FAC-13 FAC-17 FAC-19 FAC-2 FAC-24 LYR-36
```

`ticket-fac-33.md` carries <issue id="d71d5bae-fbed-487f-8e91-7f1e6f7975ce" href="https://linear.app/hevmind/issue/LYR-51/rfc-0116-six-demos-on-ce-pgvector-with-compose-pipelines-and-cpu">LYR-51</issue> and <issue id="14bd45c6-d415-4f44-ba85-6e9693ef712f" href="https://linear.app/hevmind/issue/LYR-52/06-replace-integer-hevlayer-upserted-at-with-a-date-field-retire">LYR-52</issue>, so a factory gaffer is currently a candidate to adopt an lyr issue.

## Done when

* Ownership is decided by a recorded field, never by searching plan text. If the legacy fallback must survive, it matches only records that have no `issue` **and** whose plan's `> Approved source:` header is exactly this issue's URL — not any mention anywhere in the body.
* The fallback is scoped to the issue's own instance, so no assignment can adopt another team's issue.
* Adoption is never silent: adopting an issue onto an existing record writes an event and a line the operator can see, and an approved issue that produces no assignment is a recorded problem rather than `problems: []`.
* A regression test approves an issue whose URL appears in another instance's plan body and asserts it gets its own gaffer.

Filed by reception at the operator's request, 2026-09-15.
