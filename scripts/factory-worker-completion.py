#!/usr/bin/env python3
"""Conservative completion evidence for owner-scoped terminal harvest.

This reads testimony, never establishes acceptance or authorizes a retry.
"""
import datetime
import fnmatch
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import tomllib


def timestamp(value):
    if not isinstance(value, str):
        return None
    try:
        parsed = datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))
        return parsed.timestamp() if parsed.tzinfo else None
    except ValueError:
        return None


def completion(child, instance, owner, spool, pane):
    if (child.get('instance') != instance or child.get('parent') != owner or
            not owner or not child.get('session')):
        return False, 'ambiguous ownership; inspect original dispatch ledger'
    start = timestamp(child.get('dispatched_at'))
    now = datetime.datetime.now(datetime.timezone.utc).timestamp()
    if start is None or start > now:
        return False, 'ambiguous dispatch time; inspect original launch evidence'
    completed = timestamp(child.get('completed_at'))
    if child.get('completed_at') and (completed is None or completed > now):
        return False, 'ambiguous completion time; inspect original completion evidence'
    reason = 'no durable completion; inspect worker evidence and record completion or recovery'
    proof = {'completed_at': child['completed_at']} if completed is not None and completed >= start else None
    latest = completed if proof else start
    # Read complete records only; corruption is a loud failure, never permission.
    if spool.exists():
        lines = spool.read_bytes().splitlines(keepends=True)
        prefix = hashlib.sha256()
        for index, line in enumerate(lines):
            prefix.update(line)
            try:
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise ValueError('wire record must be an object')
            except (ValueError, UnicodeDecodeError):
                if index == len(lines) - 1 and not line.endswith(b'\n'):
                    continue
                state = Path(os.environ.get('FACTORY_STATE_DIR', str(Path.home() / '.factory')))
                repair = state / 'controller/recovery/spool' / instance / (str(index + 1) + '.json')
                disposition = json.loads(repair.read_text()) if repair.exists() else {}
                if (disposition.get('spool') == str(spool.resolve()) and
                        disposition.get('prefix_sha256') == prefix.hexdigest() and
                        disposition.get('reason', '').strip()):
                    continue
                raise ValueError('invalid completed worker spool record at line ' + str(index + 1))
            if event.get('instance') != instance or event.get('from') != child['session']:
                continue
            at = timestamp(event.get('ts'))
            if at is None or at > now:
                return False, 'ambiguous wire timestamp; inspect worker spool'
            if at < start:
                continue
            kind = event.get('kind')
            if kind in ('started', 'blocked', 'failed', 'done') and at >= latest:
                latest = at
                proof = event if kind == 'done' and isinstance(event.get('text'), str) and event['text'].strip() else None
                if kind in ('blocked', 'failed'):
                    reason = kind + ': ' + str(event.get('text', '')) + '; inspect side effects and record explicit recovery; no automatic retry'
                else:
                    reason = 'worker has no durable completion; inspect worker evidence'
    # Only the visible tail is used for a veto. Historical pane prose cannot
    # supply completion. Known busy/refusal prompts override stale testimony.
    if re.search(r'(?i)(esc(?:ape)? to (?:interrupt|cancel)|(?:out of|insufficient) (?:usage )?credits|credit balance|usage limit|you.ve hit your limit)', pane):
        return False, 'working or credit-refused terminal; inspect current activity/credit failure before explicit recovery; no automatic retry'
    if proof:
        return True, json.dumps(proof, sort_keys=True)
    return False, reason


def main():
    child_path, instance, owner, config = sys.argv[1:]
    child = json.loads(Path(child_path).read_text())
    if child.get('session') != Path(child_path).stem:
        print('ledger/session mismatch; owner must inspect original launch')
        return 2
    cfg = tomllib.loads(Path(config).read_text())
    repo = child.get('repo')
    if repo not in cfg.get('repo_scope', []) or any(
            fnmatch.fnmatchcase(repo, pattern) for pattern in cfg.get('repo_scope_excludes', [])):
        print('repository outside current scope; owner must inspect scope without expanding it')
        return 2
    state = Path(os.environ.get('FACTORY_STATE_DIR', str(Path.home() / '.factory')))
    spool = Path(os.environ.get('FACTORY_EVENTS_DIR', str(state / 'events'))) / (instance + '.jsonl')
    ok, evidence = completion(child, instance, owner, spool, sys.stdin.read())
    print(evidence.replace('\n', ' '))
    return 0 if ok else 2


if __name__ == '__main__':
    sys.exit(main())
