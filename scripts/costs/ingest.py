#!/usr/bin/env python3
"""Read exact local accounting/assignment evidence; export no transcript content."""
import argparse
import datetime as dt
import json
from pathlib import Path
import sys
import tempfile
import os
import re
from attribution import Evidence, fields, ISSUE

FIELDS = ('input_tokens', 'cache_read_tokens', 'cache_creation_tokens', 'cache_creation_1h_tokens', 'output_tokens', 'reasoning_tokens')


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
    creation = raw.get('cache_write_input_tokens', raw.get('cache_creation_input_tokens', 0))
    one_hour = (raw.get('cache_creation') or {}).get('ephemeral_1h_input_tokens', 0)
    result = dict(input_tokens=raw.get('input_tokens',0)-(cached if codex else 0),
                  cache_read_tokens=cached, cache_creation_tokens=creation-one_hour,
                  cache_creation_1h_tokens=one_hour, output_tokens=raw.get('output_tokens',0),
                  reasoning_tokens=raw.get('reasoning_output_tokens',0))
    if any(not isinstance(v, int) or v < 0 for v in result.values()):
        raise ValueError('invalid token counters')
    return result


def parse(path, harness):
    row = dict(session_id='', harness=harness, model='', cwd='', start=0, end=0,
               usage_by_model={}, rate_limits=None, usage_observation='no_usage_event')
    evidence = Evidence()
    previous = {}
    requests = {}
    pricing_requests = {}
    for rec in records(path, allow_partial=True):
        if rec.get("_incomplete_tail"):
            row["accounting_error"] = "unfinished transcript record"
            continue
        evidence.read(rec)
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
                row['usage_observation'] = 'cumulative_totals'
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
            if any(u.values()):
                pricing_requests[str(len(pricing_requests))] = {'model':row['model'],'ts':ts,'usage':u}
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
                row['usage_observation'] = 'request_usage'
                requests[key] = (model, usage(m['usage']))
                pricing_requests[key] = {'model':model,'ts':ts,'usage':requests[key][1]}
    row['_requests'] = requests
    row['_pricing_requests'] = pricing_requests
    for model, u in requests.values():
        target = row['usage_by_model'].setdefault(model, dict.fromkeys(FIELDS, 0))
        for k in FIELDS:
            target[k] += u[k]
    row.update({k: sum(u[k] for u in row['usage_by_model'].values()) for k in FIELDS})
    row['total_tokens'] = sum(row[k] for k in FIELDS if k != 'reasoning_tokens')
    row.update(evidence.result())
    row['id'] = row['session_id']
    return row


def metadata(root, brief_index=None):
    """Exact transcript IDs from evals join to parent-owned live/harvest ledgers."""
    ledgers = {}
    resumed = {}
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
                # Harness-issued resume instructions identify this exact session.
                text = path.read_text()
                for sid in re.findall(r'(?:To continue this session, run .*?codex resume|Resume this session with:?\s*claude --resume)\s+([a-f0-9-]{36})', text):
                    resumed[sid] = r
            except (ValueError, KeyError):
                raise ValueError(f'{path}: invalid harvest ledger')
    if brief_index is not None:
        for ledger in ledgers.values():
            if ledger.get('brief'):
                brief_index.setdefault(ledger['brief'], []).append(ledger)
    joined = {}
    evals = root / 'evals/evals.jsonl'
    if evals.exists():
        for r in records(evals):
            sid = r.get('session')
            if not sid:
                continue
            ledger = ledgers.get(r.get('session_name'), {})
            target = joined.setdefault(sid,{})
            target.update({k:v for k,v in {**r, **ledger}.items() if v and
                           k in ('instance','role','plan','step','issue','issue_url','pr','repo')})
    for sid, ledger in resumed.items():
        joined.setdefault(sid,{}).update({k:v for k,v in ledger.items() if v and k in
            ('instance','role','plan','step','issue','issue_url','pr','repo')})
        joined[sid].setdefault('role','worker')

    # Launchers may supply an explicit transcript ID without requiring eval.
    for r in ledgers.values():
        if r.get('session_id'):
            joined[r['session_id']] = {k: v for k, v in r.items() if k in
                                      ('instance', 'role', 'plan', 'step', 'issue', 'issue_url', 'pr', 'repo')}
            joined[r['session_id']].setdefault('role', 'worker')
    # Wrapper-owned final reports and session stamps are exact gaffer IDs.
    for directory in (root/'iterations').glob('*'):
        if not directory.is_dir(): continue
        last = directory/'last.json'
        if last.exists():
            data = json.loads(last.read_text())
            if data.get('session_id'):
                joined.setdefault(data['session_id'],{}).update(instance=directory.name,role='gaffer')
        for path in directory.glob('*.log'):
            for line in path.open(errors='replace'):
                match = re.fullmatch(r'session:\s*([a-f0-9-]{36})\s*',line)
                if match: joined.setdefault(match[1],{}).update(instance=directory.name,role='gaffer')
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
    p.add_argument('--outcomes', type=Path, help='Validated scoped outcomes from this refresh for exact ledger PR joins')
    p.add_argument('--plan-root', action='append', default=[], metavar='INSTANCE=PATH',
                   help='Explicit local approved plan directory for exact instance/plan/source joins; repeatable')
    args = p.parse_args()
    if args.days <= 0:
        p.error('--days must be positive')
    cutoff = int((dt.datetime.now(dt.timezone.utc) - dt.timedelta(days=args.days)).timestamp()*1000)
    brief_index = {}
    attrs = metadata(args.factory_root, brief_index)
    plan_issues = {}
    for supplied in args.plan_root:
        instance, sep, directory = supplied.partition('=')
        if not instance or not sep or not Path(directory).is_dir():
            p.error('--plan-root requires INSTANCE=existing-directory')
        for path in Path(directory).rglob('*.md'):
            # Exact plan slug plus instance, never a similar title or another
            # line's plan. Conflicting explicit source fields remain ambiguous.
            plan_issues.setdefault((instance,path.stem),set()).update(fields(path.read_text()))
    outcome_prs = {}
    outcome_path = args.outcomes or args.factory_root/'costs/outcomes.json'
    if outcome_path.exists():
        for issue in json.loads(outcome_path.read_text()).get('issues',[]):
            for pr in issue.get('prs',[]):
                if pr.get('number'):
                    outcome_prs.setdefault((pr['repo'],str(pr['number'])),set()).add(issue['issue'])
    rows = {}
    for root, harness, pattern in ((args.codex_root, 'codex', 'rollout-*.jsonl'), (args.claude_root, 'claude_code', '*.jsonl')):
        if not root.is_dir():
            raise ValueError(f'missing harness root: {root}')
        for path in sorted(root.rglob(pattern)):
            r = parse(path, harness)
            if r['end'] < cutoff or not r['session_id']:
                continue
            exact = dict(attrs.get(r['session_id'], {}))
            for brief in r.pop('_brief_paths', []):
                candidates = [x for x in brief_index.get(brief, []) if stamp(x.get('dispatched_at')) <= r['start']]
                if candidates:
                    ledger = max(candidates, key=lambda x: stamp(x.get('dispatched_at')))
                    for key in ('instance','role','issue','issue_url','plan','step','repo','pr'):
                        if ledger.get(key): exact.setdefault(key,ledger[key])
                    r.setdefault('attribution_sources', []).append('exact_brief_ledger')
            for key, value in exact.items():
                if not value: continue
                if key == 'issue' and isinstance(value, str) and value.startswith('https://linear.app/'):
                    identifiers = set(ISSUE.findall(value))
                    if len(identifiers) == 1:
                        r.setdefault('issue_url',value)
                        value = next(iter(identifiers))
                if key == 'issue' and r.get('issue') and r['issue'] != value:
                    r['issue_candidates'] = sorted({r.pop('issue'), value})
                elif key != 'issue' or not r.get('issue_candidates'):
                    r[key] = value
            if not r.get('role'): r['role'] = 'other'
            r.setdefault('instance', '')
            if r['role'] != 'other' and not r['instance']:
                r['reported_role'] = r['role']
                r['role'] = 'other'
                r['identity_status'] = 'cached_role_without_factory_identity'
            old = rows.get(r['session_id'])
            if old is not None and harness == 'claude_code':
                combined = {**old['_requests'], **r['_requests']}
                combined_pricing = {**old['_pricing_requests'], **r['_pricing_requests']}
                earliest = min(old['start'] or r['start'], r['start'] or old['start'])
                if old['end'] > r['end']:
                    r = old
                r['start'] = earliest
                r['_requests'] = combined
                r['_pricing_requests'] = combined_pricing
                r['usage_by_model'] = {}
                for model, u in combined.values():
                    target = r['usage_by_model'].setdefault(model, dict.fromkeys(FIELDS, 0))
                    for k in FIELDS: target[k] += u[k]
                r.update({k: sum(u[k] for u in r['usage_by_model'].values()) for k in FIELDS})
                r['total_tokens'] = sum(r[k] for k in FIELDS if k != 'reasoning_tokens')
            if old is None or r['end'] >= old['end']:
                rows[r['session_id']] = r
    # Explicit subagent parent IDs are an exact linkage, never a cwd guess.
    for _ in range(8):
        changed = False
        for r in rows.values():
            parent = rows.get(r.get('parent_session_id'))
            if not parent or not parent.get('instance'): continue
            for key in ('instance','issue','issue_url','plan','step','repo'):
                if not r.get(key) and parent.get(key) and not r.get('issue_candidates'):
                    r[key] = parent[key]
                    changed = True
            if r.get('instance') and r['role']=='other': r['role']='worker'
        if not changed: break
    for r in rows.values():
        if not r.get('issue') and not r.get('issue_candidates') and r.get('plan'):
            candidates = plan_issues.get((r.get('instance'),Path(r['plan']).stem),set())
            if len(candidates)==1:
                r['issue']=next(iter(candidates))
                r.setdefault('attribution_sources',[]).append('exact_instance_plan_source')
            elif candidates: r['issue_candidates']=sorted(candidates)
        if not r.get('issue') and not r.get('issue_candidates') and r.get('pr') and r.get('repo'):
            candidates = outcome_prs.get((r['repo'],str(r['pr'])),set())
            if len(candidates)==1:
                r['issue']=next(iter(candidates))
                r.setdefault('attribution_sources',[]).append('exact_ledger_pr_outcome_reference')
            elif candidates: r['issue_candidates']=sorted(candidates)
        r.pop('_requests', None)
        r['requests'] = list(r.pop('_pricing_requests', {}).values())
        r.pop('_brief_paths', None)
        if r['usage_by_model'] and r['total_tokens']>0:
            r['model']=max(r['usage_by_model'],key=lambda m:sum(v for k,v in r['usage_by_model'][m].items() if k!='reasoning_tokens'))
        errors = []
        if r.get('identity_status') and not r.get('instance'): errors.append('factory identity not established by cached role')
        if not r['model'] or '' in r['usage_by_model']: errors.append('missing model')
        if not r['total_tokens']: errors.append('missing tokens')
        if r['role'] != 'other' and not r['instance']: errors.append('missing instance')
        if r.get('issue_candidates'): errors.append('conflicting issue assignments')
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
    audit = []
    for r in rows.values():
        if not r.get('attribution_errors') and not r.get('accounting_error'): continue
        audit.append({k:r[k] for k in ('session_id','harness','instance','role','reported_role','plan','issue_candidates',
            'attribution_sources','attribution_errors','accounting_error','usage_observation','total_tokens') if k in r})
    atomic_write(args.output.parent/'attribution-pending.json',json.dumps({'schema_version':1,'rows':audit},sort_keys=True)+'\n')
    print(json.dumps({'sessions': len(rows), 'incomplete': sum(bool(r.get('attribution_errors') or r.get('accounting_error')) for r in rows.values())}), file=sys.stderr)


if __name__ == '__main__':
    main()
