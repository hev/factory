#!/usr/bin/env python3
"""Read local harness accounting, never transcript content, into kit cost rows."""
import argparse
import datetime as dt
import json
from pathlib import Path
import sys
import tempfile
import os

FIELDS = ('input_tokens', 'cache_read_tokens', 'cache_creation_tokens', 'output_tokens', 'reasoning_tokens')


def records(path, allow_partial=False):
    with path.open() as f:
        for n, line in enumerate(f, 1):
            if line.strip():
                try:
                    yield json.loads(line)
                except ValueError as e:
                    if allow_partial and not line.endswith('\n'):
                        yield {'_incomplete_tail': True}
                        return
                    raise ValueError(f'{path}:{n}: invalid JSON') from e


def stamp(value):
    return int(dt.datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp() * 1000) if value else 0


def usage(raw, codex=False):
    cached = raw.get('cached_input_tokens' if codex else 'cache_read_input_tokens', 0)
    result = dict(zip(FIELDS, (raw.get('input_tokens', 0) - (cached if codex else 0), cached,
                              raw.get('cache_creation_input_tokens', 0), raw.get('output_tokens', 0),
                              raw.get('reasoning_output_tokens', 0))))
    if any(not isinstance(v, int) or v < 0 for v in result.values()):
        raise ValueError('invalid token counters')
    return result


def parse(path, harness):
    row = dict(session_id='', harness=harness, model='', cwd='', start=0, end=0,
               usage_by_model={}, rate_limits=None)
    previous = {}
    requests = {}
    for rec in records(path, allow_partial=True):
        if rec.get("_incomplete_tail"):
            row["accounting_error"] = "unfinished transcript record"
            continue
        ts = stamp(rec.get('timestamp', ''))
        if ts:
            row['start'] = min(row['start'] or ts, ts)
            row['end'] = max(row['end'], ts)
        if harness == 'codex':
            p = rec.get('payload', {})
            if rec.get('type') in ('session_meta', 'turn_context'):
                row['session_id'] = p.get('session_id') or (p.get('id') if rec['type'] == 'session_meta' else None) or row['session_id']
                row['cwd'] = p.get('cwd') or row['cwd']
                row['model'] = p.get('model') or row['model']
            if p.get('type') != 'token_count':
                continue
            if p.get('rate_limits'):
                row['rate_limits'] = p['rate_limits']
            info = p.get('info') or {}
            total = info.get('total_token_usage')
            if total is not None:
                # Cumulative totals repeat on rate-limit updates. Reasoning is
                # already included in output, and cache reads in input.
                delta = {k: v - previous.get(k, 0) for k, v in total.items()}
                if any(v < 0 for v in delta.values()):
                    raise ValueError(f'{path}: cumulative token counters decreased')
                previous = total
                u = usage(delta, True)
            else:
                # Without cumulative totals there is no safe way to distinguish
                # replayed last_token_usage from a second equal-sized request.
                if info.get('last_token_usage'):
                    row['accounting_error'] = 'missing cumulative token totals'
                continue
            target = row['usage_by_model'].setdefault(row['model'], dict.fromkeys(FIELDS, 0))
            for k in FIELDS:
                target[k] += u[k]
        else:
            row['session_id'] = rec.get('sessionId') or row['session_id']
            row['cwd'] = rec.get('cwd') or row['cwd']
            m = rec.get('message') or {}
            if rec.get('type') == 'assistant' and m.get('usage'):
                model = m.get('model', '')
                row['model'] = model or row['model']
                key = rec.get('requestId') or m.get('id')
                if not key:
                    raise ValueError(f'{path}: assistant usage without request identity')
                requests[key] = (model, usage(m['usage']))
    row['_requests'] = requests
    for model, u in requests.values():
        target = row['usage_by_model'].setdefault(model, dict.fromkeys(FIELDS, 0))
        for k in FIELDS:
            target[k] += u[k]
    row.update({k: sum(u[k] for u in row['usage_by_model'].values()) for k in FIELDS})
    row['total_tokens'] = sum(row[k] for k in FIELDS if k != 'reasoning_tokens')
    row['id'] = row['session_id']
    return row


def metadata(root):
    """Exact transcript IDs from evals join to parent-owned live/harvest ledgers."""
    ledgers = {}
    for path in sorted((root / 'children').glob('*.json')):
        r = json.loads(path.read_text())
        ledgers[r['session']] = r
    for path in sorted((root / 'harvest').glob('*/*.log')):
        # Harvest's commented ledger ends before the pane; never read pane data.
        lines = []
        active = False
        with path.open() as f:
            for line in f:
                if line.strip() == '# ledger:':
                    active = True
                elif active:
                    if not line.startswith('# '):
                        break
                    lines.append(line[2:])
        if lines:
            try:
                r = json.loads(''.join(lines))
                ledgers.setdefault(r['session'], r)
            except (ValueError, KeyError):
                raise ValueError(f'{path}: invalid harvest ledger')
    joined = {}
    evals = root / 'evals/evals.jsonl'
    if evals.exists():
        for r in records(evals):
            sid = r.get('session')
            if not sid:
                continue
            ledger = ledgers.get(r.get('session_name'), {})
            joined[sid] = {k: v for k, v in {**r, **ledger}.items()
                           if k in ('instance', 'role', 'plan', 'step', 'issue', 'issue_url', 'pr', 'repo')}
    # Launchers may supply an explicit transcript ID without requiring eval.
    for r in ledgers.values():
        if r.get('session_id'):
            joined[r['session_id']] = {k: v for k, v in r.items() if k in
                                      ('instance', 'role', 'plan', 'step', 'issue', 'issue_url', 'pr', 'repo')}
            joined[r['session_id']].setdefault('role', 'worker')
    sidecars = root / 'costs/attribution.jsonl'
    if sidecars.exists():
        for r in records(sidecars):
            if not r.get('session_id'):
                raise ValueError('attribution sidecar missing session_id')
            joined.setdefault(r['session_id'], {}).update({k: v for k, v in r.items() if k in
                ('instance', 'role', 'plan', 'step', 'issue', 'issue_url', 'pr', 'repo')})
    return joined


def atomic_write(path, text):
    fd, name = tempfile.mkstemp(prefix='.'+path.name, dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as f:
            f.write(text)
        os.replace(name, path)
    finally:
        if os.path.exists(name): os.unlink(name)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--codex-root', type=Path, default=Path.home()/'.codex/sessions')
    p.add_argument('--claude-root', type=Path, default=Path.home()/'.claude/projects')
    p.add_argument('--factory-root', type=Path, default=Path.home()/'.factory')
    p.add_argument('--days', type=int, default=30)
    p.add_argument('--output', type=Path, required=True)
    args = p.parse_args()
    if args.days <= 0:
        p.error('--days must be positive')
    cutoff = int((dt.datetime.now(dt.timezone.utc) - dt.timedelta(days=args.days)).timestamp()*1000)
    attrs = metadata(args.factory_root)
    rows = {}
    for root, harness, pattern in ((args.codex_root, 'codex', 'rollout-*.jsonl'), (args.claude_root, 'claude_code', '*.jsonl')):
        if not root.is_dir():
            raise ValueError(f'missing harness root: {root}')
        for path in sorted(root.rglob(pattern)):
            r = parse(path, harness)
            if r['end'] < cutoff or not r['session_id']:
                continue
            r.update(attrs.get(r['session_id'], {}))
            if not r.get('role'): r['role'] = 'other'
            r.setdefault('instance', '')
            old = rows.get(r['session_id'])
            if old is not None and harness == 'claude_code':
                combined = {**old['_requests'], **r['_requests']}
                earliest = min(old['start'] or r['start'], r['start'] or old['start'])
                if old['end'] > r['end']:
                    r = old
                r['start'] = earliest
                r['_requests'] = combined
                r['usage_by_model'] = {}
                for model, u in combined.values():
                    target = r['usage_by_model'].setdefault(model, dict.fromkeys(FIELDS, 0))
                    for k in FIELDS: target[k] += u[k]
                r.update({k: sum(u[k] for u in r['usage_by_model'].values()) for k in FIELDS})
                r['total_tokens'] = sum(r[k] for k in FIELDS if k != 'reasoning_tokens')
            if old is None or r['end'] >= old['end']:
                rows[r['session_id']] = r
    for r in rows.values():
        r.pop('_requests', None)
        errors = []
        if not r['model'] or '' in r['usage_by_model']: errors.append('missing model')
        if not r['total_tokens']: errors.append('missing tokens')
        if r['role'] != 'other' and not r['instance']: errors.append('missing instance')
        if r['role'] == 'worker' and not r.get('issue'): errors.append('missing worker issue')
        if errors: r['attribution_errors'] = errors
    args.output.parent.mkdir(parents=True, exist_ok=True)
    atomic_write(args.output, ''.join(json.dumps(rows[k], sort_keys=True)+'\n' for k in sorted(rows)))
    beats = []
    for path in sorted((args.factory_root / 'beats').glob('*.jsonl')):
        for r in records(path):
            ts = stamp(r.get('ts', ''))
            if ts >= cutoff:
                beats.append({k: v for k, v in {**r, 'timestamp': ts}.items() if k in
                              ('instance', 'session_id', 'api_usd', 'sub_usd', 'timestamp', 'cost_status')})
    beat_path = args.output.parent / 'beats.jsonl'
    atomic_write(beat_path, ''.join(json.dumps(r)+'\n' for r in beats))
    print(json.dumps({'sessions': len(rows), 'incomplete': sum(bool(r.get('attribution_errors') or r.get('accounting_error')) for r in rows.values())}), file=sys.stderr)


if __name__ == '__main__':
    main()
