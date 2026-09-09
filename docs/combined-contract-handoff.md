# Combined operator gate

PR18 combines the already-approved contract work from PR15 and PR16 with the
cost-attribution report/beat contract. It remains an operator-only merge gate;
no implementation rollout or gaffer restart is authorized by this handoff.

## Preserved policy

- [PR15](https://github.com/hev/factory/pull/15), original commit
  `6033e4dcd253ca2dea93cc585e2bd94fcb14974e`, is cherry-picked with its author and
  origin recorded. Its five substitutions reconcile eight instructions,
  unique letters a–h, and the independent-review/event references. CI backoff,
  stop-at-PR, state reporting, preview review and fallback prose are unchanged.
- [PR16](https://github.com/hev/factory/pull/16), original commit
  `e1b87b8bcb45136cd3614886864fc8a929ffc1f9`, is likewise cherry-picked with its
  author and origin. Shared targets, maintenance leases and merged-only,
  clean, unused worktree cleanup remain exactly its policy. Cleanup candidates
  survive harvest/archive; open, dirty and closed-unmerged work remains.
- Each original patch is reverse-applicable to the combined tree, confirming
  that all its changes are present. Ordered standing-instruction labels a–h
  and the complete normative diff were reviewed.

## Existing acceptance tails

[Cost plan](https://github.com/hev/factory/blob/main/plans/active/factory-inference-cost-attribution.md):
all six criteria and the pricing/beat/backfill tails are enumerated in the
[cost evidence matrix](cost-attribution-evidence.md). The
[kit implementation](https://github.com/hev/kit/pull/32) remains draft alongside
this gate. The independent [dashboard work](https://github.com/hev/kit/pull/31)
stays separate and its existing projection/latency commits are preserved.

[Preview plan, integration steps 8–9](https://github.com/hev/factory/blob/main/plans/active/preview-screenshot-verification.md#integration-and-acceptance-follow-through--2026-09-08):
step 8's exact numbering correction is included. Step 9 still needs host browser
installation/doctor/health evidence, live allowlist refusal and harvest logging,
concurrent session isolation and cleanup, and the next already-approved UI
change's worker screenshots plus independent gaffer before/after verification
and substantiated no-preview fallback. Configured preview domains remain a
prerequisite. No synthetic UI task or sibling configuration fills this tail.

[Shared-cache plan](https://github.com/hev/factory/blob/main/plans/active/worker-shared-build-caches.md):
[factory implementation PR17](https://github.com/hev/factory/pull/17) and
[lab maintenance](https://github.com/hev/lab/compare/main...impl/factory-shared-build-caches-0908)
stay separate and unchanged. Their existing
[factory fixture evidence](https://github.com/hev/factory/blob/5451ada/docs/evidence/shared-build-caches.md)
and [lab evidence](https://github.com/hev/lab/blob/0103759/docs/evidence/shared-build-caches.md)
are not live acceptance. Live rollout, representative production builds and
the one-week <20 GB worktree / >60 GB free-space measure remain unverified.
Install launcher lease support before maintenance and restart the gaffer only
through the separately authorized rollout after operator approval.

**[FAC-24](https://linear.app/hevmind/issue/FAC-24) remains unanswered:** whether
weekly idle eviction meets the operator's intended cap. Weekly eviction does
not impose a hard quota on active caches. This consolidation neither answers
the question nor changes the plan's cache-policy acceptance. The parent retains
that decision and every other tail in the existing approved work list.

PR15 and PR16 are superseded only after this combined PR contains their exact
work and the acceptance links above. No implementation PR is superseded.
