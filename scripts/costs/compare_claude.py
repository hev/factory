#!/usr/bin/env python3
"""Compare exact Claude cost-state snapshots with independently priced request usage."""
import argparse
import json
from pathlib import Path
import subprocess
import tempfile
from ingest import parse, atomic_write, FIELDS

COUNTERS = {'inputTokens':'input_tokens', 'outputTokens':'output_tokens',
            'cacheReadInputTokens':'cache_read_tokens', 'cacheCreationInputTokens':'cache_creation_tokens'}


def compare(row, state):
    gaps = []
    models = state.get('modelUsage', {})
    for model in sorted(set(models) | set(row['usage_by_model'])):
        measured = row['usage_by_model'].get(model, {})
        reported = models.get(model, {})
        for source, target in COUNTERS.items():
            actual = measured.get(target, 0)
            if target == 'cache_creation_tokens':
                actual += measured.get('cache_creation_1h_tokens', 0)
            if actual != reported.get(source, 0):
                gaps.append(dict(model=model, counter=source, request_tokens=actual, harness_tokens=reported.get(source, 0)))
        if reported.get('webSearchRequests', 0):
            gaps.append(dict(model=model, reason='non-token web search charges'))
    api, harness = row.get('api_usd'), state.get('totalCostUSD')
    error = abs(api-harness)/harness if api is not None and isinstance(harness, (int,float)) and harness > 0 else None
    complete = bool(models) and not gaps and not state.get('hasUnknownModelCost', True) and not row.get('accounting_error')
    return dict(session_id=row['session_id'], harness_usd=harness, api_usd=api,
                relative_error=error, usage_complete=complete, usage_gaps=gaps,
                accounting_error=row.get('accounting_error'),
                passed=complete and error is not None and error <= .05)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--claude-root', type=Path, required=True)
    p.add_argument('--session-records', type=Path, required=True, help='JSON array with exact session_id values to audit')
    p.add_argument('--prices', type=Path, required=True)
    p.add_argument('--hev', required=True)
    p.add_argument('--output', type=Path, required=True)
    a = p.parse_args()
    ids = {r['session_id'] for r in json.loads(a.session_records.read_text())}
    rows, states = [], {}
    with tempfile.TemporaryDirectory(prefix='cost-compare-') as tmp:
        root = Path(tmp)
        for path in sorted(a.claude_root.rglob('*.jsonl')):
            if path.stem not in ids: continue
            if path.stem in states: raise ValueError('duplicate exact session file')
            lines = path.read_text().splitlines(keepends=True)
            snapshots = [(i, json.loads(line)) for i,line in enumerate(lines) if '"cost-state"' in line]
            snapshots = [(i,r) for i,r in snapshots if r.get('type') == 'cost-state' and r.get('sessionId') == path.stem]
            if not snapshots: continue
            i, state = snapshots[-1]
            # Price only usage present at the exact cumulative cost snapshot.
            transcript = root/'transcript.jsonl'
            transcript.write_text(''.join(lines[:i+1]))
            row = parse(transcript, 'claude')
            # Claude subagent transcripts carry the exact parent sessionId.
            # Include only records bounded by the observed main transcript end;
            # never infer a join from a name or from nearby wall-clock activity.
            for child in sorted((path.parent/path.stem/'subagents').glob('*.jsonl')):
                part = parse(child, 'claude')
                if part['session_id'] != row['session_id']:
                    raise ValueError('subagent directory/session identity conflict')
                if part.get('accounting_error'):
                    row['accounting_error'] = part['accounting_error']
                if part['end'] > row['end']:
                    row['accounting_error'] = 'subagent extends past cost snapshot usage boundary'
                    continue
                for key, request in part['_pricing_requests'].items():
                    prior = row['_pricing_requests'].get(key)
                    if prior is not None and prior != request:
                        raise ValueError('conflicting exact request identity')
                    row['_pricing_requests'][key] = request
            row['requests'] = list(row.pop('_pricing_requests').values())
            row.pop('_requests')
            row['usage_by_model'] = {}
            for request in row['requests']:
                target = row['usage_by_model'].setdefault(request['model'], dict.fromkeys(FIELDS, 0))
                for field in FIELDS:
                    target[field] += request['usage'][field]
            states[path.stem] = state
            rows.append(row)
        (root/'sessions.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in rows))
        (root/'prices.toml').write_text(a.prices.read_text())
        (root/'subscriptions.toml').write_text('')
        subprocess.run([a.hev, 'cost-price', '--dir', str(root), '--output', str(root/'priced.jsonl')], check=True, capture_output=True)
        results = [compare(json.loads(line), states[json.loads(line)['session_id']]) for line in (root/'priced.jsonl').read_text().splitlines()]
    result = dict(source='exact_transcript_cost_state_prefix', requested=len(ids), found=len(results),
                  missing=sorted(ids-states.keys()), passed=sum(r['passed'] for r in results), rows=results)
    a.output.parent.mkdir(parents=True, exist_ok=True)
    atomic_write(a.output, json.dumps(result, indent=2)+'\n')
    print(json.dumps({k:v for k,v in result.items() if k not in ('rows','missing')}))
    return 0 if len(results)==len(ids) and results and all(r['passed'] for r in results) else 1

if __name__ == '__main__':
    raise SystemExit(main())
