#!/usr/bin/env python3
"""Exact-session lookup of kit's priced accounting export; never infer a price."""
import argparse
import json
import math
from pathlib import Path


def lookup(path, session):
    result = {'api_usd': None, 'sub_usd': None, 'cost_status': 'pending'}
    if not path.exists():
        return result
    with path.open() as f:
        for line in f:
            row = json.loads(line)
            if row.get('session_id') != session:
                continue
            for key in ('api_usd', 'sub_usd'):
                v = row.get(key)
                if v is not None and (not isinstance(v, (int, float)) or isinstance(v, bool) or not math.isfinite(v) or v < 0):
                    raise ValueError(f'invalid {key}')
                result[key] = v
            result['cost_status'] = 'priced' if all(result[k] is not None for k in ('api_usd', 'sub_usd')) else 'incomplete'
    return result


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--priced', type=Path, required=True)
    p.add_argument('--session', required=True)
    args = p.parse_args()
    print(json.dumps(lookup(args.priced, args.session), allow_nan=False))
