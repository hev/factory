#!/usr/bin/env python3
"""Run one complete accounting refresh; suitable for an existing hourly scheduler."""
import argparse
import datetime as dt
import fcntl
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
from ingest import atomic_write


def run(argv):
    result = subprocess.run(argv, capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(f'{Path(argv[0]).name} {Path(argv[1]).name if len(argv)>1 else ""} failed: {result.stderr.strip()[:500]}')
    return result


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--config', type=Path, required=True)
    p.add_argument('--issues', type=Path, required=True)
    p.add_argument('--dir', type=Path, required=True)
    p.add_argument('--kit-bin', required=True)
    p.add_argument('--deploy-workflows', type=Path)
    p.add_argument('--days', type=int, default=30)
    p.add_argument('--factory-root', type=Path, default=Path.home()/'.factory')
    p.add_argument('--codex-root', type=Path, default=Path.home()/'.codex/sessions')
    p.add_argument('--claude-root', type=Path, default=Path.home()/'.claude/projects')
    p.add_argument('--plan-root', action='append', default=[], metavar='INSTANCE=PATH')
    a = p.parse_args()
    if sys.version_info < (3,11): p.error('Python 3.11+ is required')
    if a.days <= 0: p.error('--days must be positive')
    a.dir.mkdir(parents=True, exist_ok=True)
    with (a.dir/'.refresh.lock').open('a') as lock:
        try: fcntl.flock(lock, fcntl.LOCK_EX|fcntl.LOCK_NB)
        except BlockingIOError: raise RuntimeError('another accounting refresh is active')
        with tempfile.TemporaryDirectory(prefix='.refresh-', dir=a.dir) as temp:
            stage = Path(temp)
            for name in ('prices.toml','subscriptions.toml'):
                shutil.copyfile(a.dir/name, stage/name)
            here = Path(__file__).parent
            command = [sys.executable,str(here/'outcomes.py'),'--config',str(a.config),'--issues',str(a.issues),
                       '--days',str(a.days),'--output',str(stage/'outcomes.json')]
            if a.deploy_workflows: command += ['--deploy-workflows',str(a.deploy_workflows)]
            run(command)
            ingest = [sys.executable,str(here/'ingest.py'),'--days',str(a.days),'--factory-root',str(a.factory_root),
                 '--codex-root',str(a.codex_root),'--claude-root',str(a.claude_root),'--output',str(stage/'sessions.jsonl'),
                 '--outcomes',str(stage/'outcomes.json')]
            for supplied in a.plan_root: ingest += ['--plan-root',supplied]
            run(ingest)
            run([a.kit_bin,'cost-price','--dir',str(stage),'--output',str(stage/'priced.jsonl')])
            # Publish only after every producer succeeded. Each file is atomic;
            # the manifest is the completion signal for the whole generation.
            for name in ('sessions.jsonl','beats.jsonl','outcomes.json','priced.jsonl','attribution-pending.json'):
                atomic_write(a.dir/name,(stage/name).read_text())
            report = dict(schema_version=1, refreshed_at=int(dt.datetime.now(dt.timezone.utc).timestamp()*1000),
                          sessions=sum(1 for _ in (stage/'sessions.jsonl').open()), days=a.days)
            atomic_write(a.dir/'refresh.json',json.dumps(report)+'\n')
            print(json.dumps(report))


if __name__ == '__main__':
    try: main()
    except (RuntimeError, OSError, ValueError) as e:
        print(str(e),file=sys.stderr)
        sys.exit(1)
