# Offline artifact privacy remediation

`factory privacy plan OFFLINE_ROOT request.json plan.json` validates explicit
byte ranges and writes a content-free plan. It is a dry run: artifacts stay
unchanged. `factory privacy apply OFFLINE_ROOT plan.json receipt.json` applies
that plan and writes a content-free receipt. Paths are relative to the offline
root; offsets are zero-based, end-exclusive UTF-8 byte offsets. Each range is
replaced with the same number of ASCII `x` bytes. Select whole UTF-8 characters.
The request is version 1 with `targets`, each containing `path`,
`before_sha256` and `ranges` (`start`, `end`). No search or automatic discovery
is provided. Plans and receipts contain file digests, paths and offsets, never
original text. Treat digests as sensitive metadata; outputs have mode 0600.

The exact allowlist is `sessions/<six-base32-id>/prompt.md`,
`sessions/<six-base32-id>/log.jsonl` and `jobs/<six-base32-id>/log.md`.
JSONL edits only change literal string content in `text` or `result` fields;
keys, scalars, escapes and line boundaries cannot change. Job log entry
headings are preserved verbatim. Other artifacts,
including meta/state, job specs, inboxes and the global events stream, are
unsupported. Independently appended job logs require their own explicit target
and digest; session edits do not propagate. No association is inferred.

Before use, stop all writers, readers with caches, tick, runners and harnesses
against this root, and create `.privacy-offline` in it. This marker attests that
the operator established exclusive offline access; the command cannot verify
external quiescence. Its lock only excludes other privacy commands. Symlinks,
duplicate paths, overlapping/unbounded ranges, stale hashes and files over
16 MiB fail closed. At most 32 files and 128 ranges per file are accepted.
Never point this command at a live factory home. This release does not automate
shutdown or activation. The operator must build/install the reviewed binary
on the artifact-owning host and establish offline access before a future run.

The durable plan is the recovery journal: it records before and after digests
before apply. Each artifact is replaced atomically, with its permission bits
and byte length preserved. Multi-file apply is not atomic. If interrupted,
rerun the same plan with a new receipt path: already-applied hashes are accepted,
unchanged originals are transformed, and any third state is refused. No original
backup is created. A receipt failure can occur after writes; recover the same
way. Preserve the plan until all artifacts are verified. Resume ordinary
factory operation only after recovery completes. Existing hard links, external
backups and process caches are outside coverage; copies must be inventoried
separately. File owner and group must match the account performing the operation; otherwise
the command refuses the artifact.

Every result reports `indexed_copies: unknown`,
`native_harness_sources: unsupported`, and `full_remediation: false`.
No index API or native Claude/Codex source is read or changed. Indexed copies,
harness history, event/state/spec copies, backups and external exports require
separate supported tools and verification. A receipt proves only the enumerated
artifact transformations, never full historical remediation.
