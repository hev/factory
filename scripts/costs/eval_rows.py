#!/usr/bin/env python3
"""Adapt factory evaluation JSONL to kit's public `hev eval put` input.

Structured findings become canonical JSON strings, preserving every field.
Output contains private evaluation prose; pipe only to an authorized store.
"""
import argparse
import json
from pathlib import Path
import sys
from ingest import records

FIELDS = ('session', 'ts', 'role', 'instance', 'host', 'marks', 'poor', 'summary', 'evidence')


def adapt(row):
    if not isinstance(row, dict) or any(key not in row for key in ('session', 'ts', 'marks')):
        raise ValueError('evaluation requires session, ts and marks')
    findings = row.get('findings', [])
    if findings is None:
        findings = []
    if not isinstance(findings, list) or any(not isinstance(value, (str, dict)) for value in findings):
        raise ValueError('findings must be strings or objects')
    result = {key:row[key] for key in FIELDS if key in row}
    result['findings'] = [json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False)
                          if isinstance(value, dict) else value for value in findings]
    return result


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('input', type=Path)
    a = p.parse_args()
    for row in records(a.input):
        print(json.dumps(adapt(row), allow_nan=False))

if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
