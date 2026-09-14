#!/usr/bin/env python3
"""Deterministic task dispatch for integration with the event controller.

The boot subcommand runs inside a new worker terminal. Its durable receipt is
an at-most-once fence across tmux creation and controller crash/replay.
"""
import fcntl
import fnmatch
import json
import os
from pathlib import Path
import re
import subprocess
import sys


DEFINITION = ('id', 'repo', 'worktree', 'brief', 'kind', 'after')


def in_scope(cfg, repo):
    return repo in cfg.get('repo_scope', []) and not any(
        fnmatch.fnmatchcase(repo, pattern) for pattern in cfg.get('repo_scope_excludes', []))


def ledger_dir(c):
    return Path(os.environ.get('FACTORY_LEDGER_DIR', str(c.STATE / 'children')))


def save(c, record):
    c.s.write(c.STATE / 'gaffers' / (record['session'] + '.json'), record)


def validate_lane(c, task):
    lane = Path(task['worktree'])
    if not lane.is_absolute() or lane.resolve() != lane or not (lane / '.git').is_file():
        raise ValueError('task lane must be a canonical linked worktree')
    top = c.s.run('git', '-C', lane, 'rev-parse', '--show-toplevel').stdout.strip()
    if Path(top).resolve() != lane:
        raise ValueError('task lane must be the worktree root')
    origin = c.s.run('git', '-C', lane, 'remote', 'get-url', 'origin').stdout.strip()
    match = re.fullmatch(r'(?:https://github.com/|git@github.com:)([\w.-]+/[\w.-]+?)(?:\.git)?', origin)
    if not match or match[1].lower() != task['repo'].lower():
        raise ValueError('task lane origin does not match repository')
    brief = Path(task['brief'])
    if not brief.is_absolute() or not brief.is_file() or not brief.read_text().strip():
        raise ValueError('task needs a nonempty absolute brief file')


def overlap(a, b):
    a, b = Path(a).resolve(), Path(b).resolve()
    return a == b or a in b.parents or b in a.parents


def paused(c, record):
    return c.source_paused(record) if hasattr(c, 'source_paused') else record.get('source_paused', False)


def require_owner(c, session):
    if (os.environ.get('FACTORY_ROLE') != 'gaffer' or
            os.environ.get('FACTORY_GAFFER_SESSION') != session or
            os.environ.get('FACTORY_CONTROLLER_TURN') != '1'):
        raise ValueError('only the owning gaffer event turn may resolve work')
    record = c.read(c.STATE / 'gaffers' / (c.s.name(session) + '.json'))
    if c.s.held(record['instance']) or paused(c, record):
        raise ValueError('assignment held or source paused')
    key = os.environ.get('FACTORY_CONTROLLER_EVENT', '')
    current = c.read(c.BASE / 'queues' / session / (c.digest(key) + '.json'), {})
    if current.get('status') != 'running':
        raise ValueError('resolution requires a running durable decision event')
    return record, key


def resolve_task(c, session, ident, evidence):
    if not evidence or not evidence.strip():
        raise ValueError('task resolution requires evidence or replacement task IDs')
    with c.gate(c.BASE / 'dispatch.lock'):
        record, key = require_owner(c, session)
        task = next(t for t in record['tasks'] if t['id'] == ident)
        if task['status'] != 'blocked':
            raise ValueError('only blocked tasks need explicit resolution')
        task.update(status='done', done_claim='decision:' + key,
                    disposition={'event': key, 'evidence': evidence, 'at': c.s.stamp()})
        task.pop('attention', None)
        save(c, record)


def reap(c, record):
    env = dict(os.environ, FACTORY_GAFFER_SESSION=record['session'],
               FACTORY_STATE_DIR=str(c.STATE), FACTORY_LEDGER_DIR=str(ledger_dir(c)),
               FACTORY_HARVEST_DIR=str(c.STATE / 'harvest'),
               FACTORY_DEFER_WORKTREE_CLEANUP='0' if record.get('delivery', {}).get('status') == 'delivered'
               and all(t['status'] == 'done' for t in record.get('tasks', [])) else '1')
    result = c.s.run(c.ROOT / 'scripts/factory-reap.sh', record['instance'],
                     env=env, check=False, timeout=90)
    log = c.BASE / 'reaper' / (record['session'] + '.log')
    log.parent.mkdir(parents=True, exist_ok=True)
    log.write_text(result.stdout + result.stderr)
    if result.returncode:
        raise RuntimeError('owner-scoped reaper failed; preserve workers and lanes; see ' + str(log))


def commission(c, session, tasks):
    if (os.environ.get('FACTORY_ROLE') != 'gaffer' or
            os.environ.get('FACTORY_GAFFER_SESSION') != session or
            os.environ.get('FACTORY_CONTROLLER_TURN') != '1'):
        raise ValueError('only the owning gaffer event turn commissions tasks')
    with c.gate(c.BASE / 'dispatch.lock'):
        record = c.read(c.STATE / 'gaffers' / (c.s.name(session) + '.json'))
        cfg = c.s.local_configs()[record['instance']]
        if record.get('owner', session) != session:
            raise ValueError('assignment has a different owner')
        if any(r['session'] != session and r['status'] != 'retired' and
               Path(r['plan']).resolve() == Path(record['plan']).resolve() for r in c.s.records()):
            raise ValueError('plan already has another owner')
        if record['status'] == 'retired' or record.get('transport') != 'exec':
            raise ValueError('commission requires an active exec assignment')
        if c.s.held(record['instance']) or paused(c, record) or (c.STATE / 'winddown' / record['instance']).exists():
            raise ValueError('assignment held, paused or winding down')
        if not isinstance(tasks, list) or not tasks:
            raise ValueError('commission needs a nonempty bounded task list')
        old = {t['id']: t for t in record.get('tasks', [])}
        seen, lanes, normalized = set(), dict(record.get('worktree_lanes', {})), []
        for task in tasks:
            t = {k: task[k] for k in DEFINITION}
            ident = c.s.name(t['id'])
            if ident in seen or not isinstance(t['after'], list) or not set(t['after']) <= seen:
                raise ValueError('tasks need unique IDs and dependencies on earlier tasks')
            if t['kind'] not in ('implementation', 'review') or not in_scope(cfg, t['repo']):
                raise ValueError('task kind or repository outside scope')
            validate_lane(c, t)
            for other in c.s.records():
                if other['session'] != session and other['status'] != 'retired':
                    if any(overlap(t['worktree'], lane) for lane in other.get('worktree_lanes', [])):
                        raise ValueError('worktree lane owned by another assignment')
            for p in ledger_dir(c).glob('*.json'):
                child = c.read(p)
                if child.get('worktree') and overlap(t['worktree'], child['worktree']):
                    if child.get('parent') != session or child.get('task_id') not in old:
                        raise ValueError('lane has a worker requiring explicit adoption')
            if ident in old:
                if old[ident]['status'] != 'pending' and any(t[k] != old[ident][k] for k in DEFINITION):
                    raise ValueError('existing task definitions are immutable; append a new attempt')
                t = dict(old[ident], **t)
            else:
                t.update(status='pending', session='worker-' + record['instance'] + '-' +
                         c.digest([session, ident])[:20])
            normalized.append(t)
            seen.add(ident)
            lanes[t['worktree']] = t['repo']
        if not set(old) <= seen:
            raise ValueError('task history cannot be removed')
        for t in normalized:
            if t['kind'] == 'implementation' and not any(
                    r['kind'] == 'review' and t['id'] in r['after'] for r in normalized):
                raise ValueError('each implementation needs a dependent independent review')
        record.update(owner=session, repo_scope=sorted(set(t['repo'] for t in normalized)),
                      worktree_lanes=lanes, tasks=normalized)
        save(c, record)


def worker_command(cfg, cwd, brief):
    harness = cfg.get('worker_harness', 'claude')
    if harness == 'codex':
        cmd = ['codex', '-a', 'never', '-s', 'danger-full-access', '-C', cwd]
        if cfg.get('worker_effort'):
            cmd += ['-c', 'model_reasoning_effort=' + json.dumps(cfg['worker_effort'])]
    elif harness == 'claude':
        cmd = ['claude']
        if cfg.get('worker_effort'):
            cmd += ['--effort', cfg['worker_effort']]
    else:
        raise ValueError('unsupported worker harness')
    if cfg.get('worker_model'):
        cmd += ['--model', cfg['worker_model']]
    return cmd + [f'Read {brief}, acknowledge the assignment, and complete that bounded task.']


def reservation(c, record, task, cfg):
    session = task['session']
    directory = c.BASE / 'workers' / session
    directory.mkdir(parents=True, exist_ok=True)
    brief = directory / 'brief.md'
    if not brief.exists():
        brief.write_text(
            f'You are {session}, directed ONLY by {record["session"]}.\n'
            f'Approved plan: {record["plan"]}. Task: {task["id"]}.\n'
            f'Worktree: {task["worktree"]}. Task kind: {task["kind"]}.\n'
            f'Read {c.ROOT}/contracts/factory-loop.md step 3 and contracts/ci.md; '
            'all eight standing worker instructions apply. Read repo AGENTS.md and learnings. '
            'Self-review and rerun acceptance before opening a PR. Never merge, recruit, '
            'contact the operator/Linear or write the parent-owned ledger. '
            'Review tasks inspect read-only and report independent acceptance evidence.\n'
            f'Wire: {c.ROOT}/scripts/factory-say.sh {record["instance"]} {session} '
            '<started|blocked|failed|pr|done|note> "<one line>"\n'
            f'CI handoff: {c.ROOT}/factory ci wait {record["instance"]} {session} {task["repo"]} <PR>\n'
            'Report watch ID and remaining verification once, then yield. No model polling.\n'
            f'Preview domains: {json.dumps(cfg.get("preview_domains", []))}. '
            'Use the configured preview evidence rules, or name the test/build stand-in.\n\n' +
            Path(task['brief']).read_text())
    launch = directory / 'launch.json'
    if not launch.exists():
        c.s.write(launch, dict(command=worker_command(cfg, task['worktree'], brief),
                              session=session, owner=record['session']))
    child = ledger_dir(c) / (session + '.json')
    existing = c.read(child)
    if existing and (existing.get('parent') != record['session'] or existing.get('task_id') != task['id']):
        raise ValueError('worker identity already owned')
    if not existing:
        c.s.write(child, dict(session=session, instance=record['instance'], parent=record['session'],
                  repo=task['repo'], plan=Path(record['plan']).stem, step=task['id'], task_id=task['id'],
                  brief=str(brief), worktree=task['worktree'], dispatched_at=c.s.stamp()))
    return launch


def launch(c, record, task, cfg):
    path = reservation(c, record, task, cfg)
    if (path.parent / 'started.json').exists():
        return  # boot receipt, not process existence, fences the harness
    env = dict(os.environ, FACTORY_ROLE='gaffer', FACTORY_INSTANCE=record['instance'],
               FACTORY_GAFFER_SESSION=record['session'])
    result = c.s.run(c.ROOT / 'scripts/factory-as.sh', 'worker', '--', 'tmux', 'new-session',
                    '-d', '-s', task['session'], '-c', task['worktree'], '-x', '180', '-y', '48',
                    '-e', 'FACTORY_TASK_LAUNCH=' + str(path),
                    '-e', 'FACTORY_STATE_DIR=' + str(c.STATE),
                    '-e', 'FACTORY_LEDGER_DIR=' + str(ledger_dir(c)),
                    '-e', 'FACTORY_EVENTS_DIR=' + os.environ.get('FACTORY_EVENTS_DIR', str(c.STATE / 'events')),
                    '--', sys.executable, Path(__file__).resolve(), 'boot', path,
                    env=env, check=False)
    if result.returncode:
        identity = c.s.run('tmux', 'show-environment', '-t', '=' + task['session'],
                           'FACTORY_TASK_LAUNCH', check=False)
        if identity.returncode or identity.stdout.strip() != 'FACTORY_TASK_LAUNCH=' + str(path):
            raise RuntimeError('worker launch failed or session belongs to another launch')


def terminal_exists(c, session):
    return c.s.run('tmux', 'has-session', '-t', '=' + session, check=False).returncode == 0


def boot(path):
    path = Path(path)
    with (path.parent / 'boot.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        receipt = path.parent / 'started.json'
        if receipt.exists():
            return 0
        # Exclusive creation is durable before the external operation. A crash
        # here is an explicit failed attempt, never a second harness on replay.
        with receipt.open('x') as f:
            json.dump({'pid': os.getpid()}, f)
            f.flush(); os.fsync(f.fileno())
        cmd = json.loads(path.read_text())['command']
        return subprocess.call(cmd)


def fail(c, record, task, reason, key):
    task.update(status='blocked', attention=reason)
    c.event(record['session'], key, {'kind': 'worker-failed', 'task': task['id'], 'reason': reason})
    save(c, record)


def observe(c, record):
    """Turn owned wire/CI facts into state and narrowly scoped judgment events."""
    tasks = {t['session']: t for t in record.get('tasks', [])}
    spool = Path(os.environ.get('FACTORY_EVENTS_DIR', str(c.STATE / 'events'))) / (record['instance'] + '.jsonl')
    if spool.exists():
        lines = spool.read_text().splitlines(keepends=True)
        for index, line in enumerate(lines):
            try:
                wire = json.loads(line)
            except json.JSONDecodeError:
                if index == len(lines) - 1 and not line.endswith('\n'):
                    continue  # a writer can still be appending its last line
                raise ValueError('invalid completed worker spool record')
            t = tasks.get(wire.get('from'))
            if not t or t['status'] == 'pending' or wire.get('instance') != record['instance']:
                continue
            key = 'wire:' + c.digest(wire)
            if key in t.get('wire_seen', []):
                continue
            kind = wire.get('kind')
            if kind in ('blocked', 'failed'):
                # Persist event before its consumption marker: crash can replay
                # the same key, never lose a decision between two file writes.
                fail(c, record, t, wire.get('text', kind), key)
            elif kind == 'done' and t['status'] not in ('blocked', 'done'):
                t['done_claim'] = key
            elif kind == 'pr':
                match = re.fullmatch(r'https://github.com/([^/]+/[^/]+)/pull/(\d+)', wire.get('text', '').strip())
                if match and match[1] == t['repo']:
                    child = ledger_dir(c) / (t['session'] + '.json')
                    data = c.read(child)
                    if data and data.get('parent') == record['session']:
                        data['pr'] = int(match[2]); c.s.write(child, data)
            t.setdefault('wire_seen', []).append(key)
            save(c, record)
    for t in tasks.values():
        watches = [c.read(p) for p in (c.STATE / 'ci' / record['instance']).glob('*.json')]
        watches = [w for w in watches if w.get('worker') == t['session'] and
                   w.get('ledger', {}).get('parent') == record['session']]
        for w in watches:
            if w['state'] == 'waiting':
                t['attention'] = 'waiting for CI ' + w['id']
            elif w['state'] == 'passed':
                t.setdefault('done_claim', 'ci:' + w['id'])
                t.setdefault('ci_dispositions', {})[w['id']] = 'handoff to independent review/final acceptance'
                save(c, record)
                c.s.run(c.ROOT / 'factory', 'ci', 'ack', record['instance'], w['id'])
            else:
                fail(c, record, t, 'CI ' + w['state'] + ': ' + w['id'], 'ci:' + w['id'])
        if t.get('done_claim') and t['status'] != 'blocked' and not any(w['state'] == 'waiting' for w in watches):
            t.update(status='done'); t.pop('attention', None)
        elif t['status'] == 'running' and not watches and not terminal_exists(c, t['session']):
            fail(c, record, t, 'worker session disappeared', 'missing:' + t['session'])
    for t in tasks.values():
        if t['status'] == 'done':
            path = ledger_dir(c) / (t['session'] + '.json')
            child = c.read(path)
            if child and child.get('parent') == record['session']:
                child.setdefault('completed_at', c.s.stamp())
                c.s.write(path, child)
    save(c, record)
    if tasks and all(t['status'] == 'done' for t in tasks.values()):
        c.event(record['session'], 'final:' + c.digest([t['id'] for t in tasks.values()]),
                {'kind': 'final-done', 'tasks': [t['id'] for t in tasks.values()]})
    for p in (c.STATE / 'gaffers' / (record['session'] + '.inbox')).glob('*.json'):
        c.event(record['session'], 'inbox:' + p.name + ':' + c.digest(p.read_text()),
                {'kind': 'steering', 'path': str(p)})


def tend(c, session, cfg, fenced=False):
    with c.gate(c.BASE / 'dispatch.lock'):
        record = c.read(c.STATE / 'gaffers' / (session + '.json'))
        reconcile_observations(c, record, fenced=fenced)
        if record['status'] == 'retired':
            return
        if record.get('tasks') and record.get('owner') != session:
            raise ValueError('task list owner differs from assignment')
        observe(c, record)
        record.pop('dispatch_attention', None)
        delivered = record.get('delivery', {}).get('status') == 'delivered' and all(
            t['status'] == 'done' for t in record.get('tasks', []))
        if record.get('tasks') and not c.s.held(record['instance']) and (not paused(c, record) or delivered):
            reap(c, record)
        if not record.get('tasks'):
            record['dispatch_attention'] = 'commission task list required'
        elif c.s.held(record['instance']) or paused(c, record):
            record['dispatch_attention'] = 'held or source paused'
        elif (c.STATE / 'winddown' / record['instance']).exists():
            record['dispatch_attention'] = 'winding down; no new workers'
        elif (not fenced and c.active(session)) or any(c.read(p)['status'] != 'done' for p in (c.BASE / 'queues' / session).glob('*.json')):
            record['dispatch_attention'] = 'awaiting assignment judgment'
        else:
            for t in record.get('tasks', []):
                if t['status'] not in ('pending', 'reserved'):
                    continue
                if any(x['status'] != 'done' for x in record['tasks'] if x['id'] in t['after']):
                    continue
                validate_lane(c, t)
                if any(r['session'] != session and r['status'] != 'retired' and
                       any(overlap(t['worktree'], lane) for lane in r.get('worktree_lanes', []))
                       for r in c.s.records()):
                    raise ValueError('worktree lane owned by another assignment')
                if not in_scope(cfg, t['repo']):
                    raise ValueError('task repository left scope')
                live = set(c.s.run('tmux', 'list-sessions', '-F', '#S', check=False).stdout.splitlines())
                children = [c.read(p) for p in ledger_dir(c).glob('*.json')]
                running = [x for r in c.s.records() for x in r.get('tasks', []) if x['status'] in ('reserved', 'running') and x['session'] != t['session']]
                occupied = {x['session'] for x in running} | {n for n in live if n.startswith('worker-') and n != t['session']}
                repo_workers = {x['session'] for x in running if x['repo'] == t['repo']} | {x['session'] for x in children if x['session'] in occupied and x.get('repo') == t['repo']}
                unknown = occupied - {x['session'] for x in running} - {x['session'] for x in children}
                if len(occupied) >= 8 or len(repo_workers) >= 2 or unknown:
                    record['dispatch_attention'] = 'worker capacity or unowned live worker'; break
                if any(overlap(t['worktree'], x['worktree']) for x in running):
                    record['dispatch_attention'] = 'worktree lane occupied'; continue
                t['status'] = 'reserved'; save(c, record)
                try:
                    launch(c, record, t, cfg)
                    t['status'] = 'running'
                except Exception as exc:
                    fail(c, record, t, 'launch failed: ' + type(exc).__name__, 'launch:' + t['session'])
                    break
                save(c, record)
        save(c, record)


def reconcile_observations(c, record, fenced=False):
    """Retain historical provenance; retire only known mechanical observations.

    Never reclassify a failed decision as an observation because its payload is
    old, or reset attempts to get around a capacity/auth failure.
    """
    if not fenced and c.active(record['session']):
        return
    for path in (c.BASE / 'queues' / record['session']).glob('*.json'):
        e = c.read(path)
        if e['status'] == 'done':
            continue
        kind = e.get('payload', {}).get('kind')
        if kind in ('floor-change', 'resync'):
            e.update(status='done', disposition='reconciled observation; no model required',
                     reconciled_at=c.s.stamp())
            c.s.write(path, e)
        elif record['status'] == 'retired':
            e.update(status='blocked', attention='retired assignment has an unhandled decision')
            c.s.write(path, e)


def queue_health(c, record):
    """One classification per unconsumed event, including newly queued events."""
    result = []
    running = c.active(record['session'])
    for path in sorted((c.BASE / 'queues' / record['session']).glob('*.json')):
        e = c.read(path)
        if e['status'] == 'done':
            continue
        owns_event = running and e['status'] in ('pending', 'running')
        item = dict(session=record['session'], event=e['key'], status='running' if owns_event else 'ATTENTION')
        if not owns_event:
            if record['status'] == 'retired': reason = 'retired assignment has an unhandled decision'
            elif c.s.held(record['instance']): reason = 'factory held'
            elif paused(c, record): reason = 'source paused'
            elif record.get('transport') != 'exec': reason = 'legacy transport needs adoption'
            elif e['status'] == 'blocked': reason = e.get('attention', 'failed model acknowledgment; owner recovery required')
            elif e['status'] == 'running': reason = 'runner disappeared; owner recovery required'
            elif e.get('not_before', 0) > c.time.time(): reason = 'deferred event requires explicit owner recovery'
            else: reason = 'pending runner or controller turn capacity'
            item['reason'] = reason
        result.append(item)
    return result


if __name__ == '__main__':
    if len(sys.argv) != 3 or sys.argv[1] != 'boot':
        raise SystemExit('usage: factory-dispatch.py boot <launch.json>')
    sys.exit(boot(sys.argv[2]))
