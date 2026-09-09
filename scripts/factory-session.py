#!/usr/bin/env python3
"""Persistent foreman and assignment-lived gaffers on the default tmux server."""
import argparse
import contextlib
import datetime
import fcntl
import json
import os
from pathlib import Path
import re
import shlex
import socket
import subprocess
import sys
import time
import tomllib

# launchd and non-interactive SSH do not load the operator's shell PATH.
os.environ['PATH'] = '/opt/homebrew/bin:/usr/local/bin:' + os.environ.get('PATH', '')

ROOT = Path(__file__).resolve().parent.parent
STATE = Path(os.environ.get('FACTORY_STATE_DIR', str(Path.home() / '.factory')))


def run(*args, check=True, **kwargs):
    return subprocess.run([str(a) for a in args], check=check, text=True,
                          capture_output=True, **kwargs)


def stamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def write(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix('.tmp')
    tmp.write_text(json.dumps(value, indent=2) + '\n')
    tmp.replace(path)


def name(value):
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_-]*', value):
        raise ValueError('names must contain only letters, numbers, underscores and hyphens')
    return value


def configs():
    result = {}
    for p in sorted((ROOT / 'factories').glob('*.toml')):
        if p.stem != 'example':
            result[name(p.stem)] = tomllib.loads(p.read_text())
    return result


def at_home(cfg):
    host = os.environ.get('FACTORY_HOSTNAME_OVERRIDE', socket.gethostname()).split('.')[0].lower()
    return bool(cfg.get('home_host')) and cfg['home_host'].lower() == host


def local_configs():
    return {i: c for i, c in configs().items() if at_home(c) and c.get('runtime') == 'sessions'}


def alive(session):
    return run('tmux', 'has-session', '-t', '=' + session, check=False).returncode == 0


@contextlib.contextmanager
def lock():
    STATE.mkdir(parents=True, exist_ok=True)
    with (STATE / 'sessions.lock').open('a') as f:
        fcntl.flock(f, fcntl.LOCK_EX)
        yield


def records():
    return [json.loads(p.read_text()) for p in sorted((STATE / 'gaffers').glob('*.json'))]


def held(instance):
    return (Path(os.environ.get('FACTORY_HOLDS_DIR', str(STATE / 'holds'))) / instance).exists()


def launch(role, session, cfg, cwd, prompt, instance=''):
    harness = cfg.get('harness', 'claude')
    model = cfg.get('model', '')
    effort = cfg.get('effort', '')
    if harness == 'codex':
        cmd = ['codex', '-a', 'never', '-s', 'danger-full-access', '-C', str(cwd)]
        if effort:
            cmd += ['-c', 'model_reasoning_effort=' + json.dumps(effort)]
    elif harness == 'claude':
        cmd = ['claude']
        if effort:
            cmd += ['--effort', effort]
    else:
        raise ValueError('unsupported session harness: ' + harness)
    # Each bridge reads the configured bot grant per request, so OAuth renewal
    # does not require replacing a long-running manager's context.
    cs = local_configs() if role == 'foreman' else {instance: cfg}
    servers = {}
    for inst, config in cs.items():
        if config.get('linear_team'):
            server = config.get('linear_mcp_server', 'linear')
            servers.setdefault(server, {'command': 'python3',
                'args': [str(ROOT / 'scripts/factory-mcp.py'), inst]})
    if harness == 'codex':
        cmd += ['-c', 'projects={' + json.dumps(str(cwd)) + '={trust_level="trusted"}}']
        # Override values are TOML. Quoting a segment in a -c dotted key is
        # treated literally by this CLI, producing an invalid server name.
        entries = []
        for server, config in servers.items():
            entries.append(json.dumps(server) + '={' + ','.join(
                key + '=' + json.dumps(value) for key, value in config.items()) + '}')
        cmd += ['-c', 'mcp_servers={' + ','.join(entries) + '}']
    elif servers:
        cmd += ['--strict-mcp-config', '--mcp-config', json.dumps({'mcpServers': servers})]
    if model:
        cmd += ['--model', model]
    cmd += [prompt]
    env = dict(os.environ, FACTORY_INSTANCE=instance)
    if role == 'gaffer':
        env['FACTORY_GAFFER_SESSION'] = session
    # tmux takes multiple command arguments directly; never interpolate the prompt
    # into a shell command. factory-as resolves identity at session creation.
    run(ROOT / 'scripts/factory-as.sh', role, '--', 'tmux', 'new-session',
        '-d', '-s', session, '-c', cwd, '-x', '180', '-y', '48', *cmd, env=env)


def ensure_foreman():
    cs = local_configs()
    if not cs:
        raise ValueError('no sessions factory names this machine as home_host')
    if alive('foreman'):
        return False
    cfg = dict(next(iter(cs.values())))
    for key in ('harness', 'model', 'effort'):
        override = os.environ.get('FOREMAN_' + key.upper())
        if override:
            cfg[key] = override
    cwd = STATE / 'foreman'
    cwd.mkdir(parents=True, exist_ok=True)
    prompt = (f'You are the operational foreman. Read {ROOT}/contracts/roles.md and '
              f'{ROOT}/contracts/foreman-charter.md and follow them exactly. '
              f'Factory checkout: {ROOT}. State directory: {STATE}. '
              'Read existing desk-notes.md and notes.md here if present. Reconcile the '
              'floor and approval sources now, preserving holds and existing workers; '
              'then write ready.json and wait for operator messages or timer wakes. '
              'You act as the factory identity even when the operator talks directly to you.')
    launch('foreman', 'foreman', cfg, cwd, prompt)
    write(cwd / 'session.json', {'started_at': time.time()})
    return True


def wake(session, message):
    # A durable inbox is authoritative. Never inject a timer wake into model
    # startup, an active turn or a composer holding a queued message.
    pane = run('tmux', 'capture-pane', '-t', '=' + session + ':', '-p').stdout
    footer = '\n'.join(pane.splitlines()[-8:]).lower()
    if 'esc to interrupt' in footer or 'tab to queue message' in footer:
        return
    if session == 'foreman':
        birth = STATE / 'foreman/session.json'
        if birth.exists() and time.time() - json.loads(birth.read_text())['started_at'] < 60:
            return
    run('tmux', 'send-keys', '-t', '=' + session + ':', '-l', message)
    run('tmux', 'send-keys', '-t', '=' + session + ':', 'Enter')


def start_gaffer(instance, slug, plan):
    if os.environ.get('FACTORY_ROLE') != 'foreman':
        raise ValueError('only the foreman commissions gaffers')
    instance, slug = name(instance), name(slug)
    cfg = configs()[instance]
    if not at_home(cfg):
        raise ValueError('refusing gaffer start away from home_host')
    if cfg.get('runtime') != 'sessions':
        raise ValueError('gaffer assignments require runtime = "sessions"')
    if held(instance) or (STATE / 'winddown' / instance).exists():
        raise ValueError('factory is held or winding down')
    workspace = Path(cfg['workspace_path']).expanduser().resolve()
    plan = Path(plan).expanduser().resolve()
    if plan.parent != workspace / 'plans/active' or plan.suffix != '.md' or not plan.is_file():
        raise ValueError('assignment must name a file in this workspace plans/active/')
    session = f'gaffer-{instance}-{slug}'
    for record in records():
        if record['instance'] == instance and record['plan'] == str(plan) and record['status'] != 'retired':
            if record['session'] != session:
                raise ValueError('plan already has a gaffer: ' + record['session'])
            if alive(session):
                return session
    path = STATE / 'gaffers' / (session + '.json')
    if path.exists():
        previous = json.loads(path.read_text())
        if previous['plan'] != str(plan):
            raise ValueError('session name already belongs to another plan')
    elif alive(session):
        raise ValueError('session exists without assignment; reconcile before starting')
    record = dict(session=session, instance=instance, plan=str(plan), status='starting',
                  manager='foreman', assigned_at=stamp())
    write(path, record)
    prompt = (f'You are {session}, commissioned by foreman for instance {instance}. '
              f'Read {ROOT}/contracts/roles.md and {ROOT}/contracts/gaffer-charter.md '
              f'and follow them exactly. Your sole assignment is {plan}. '
              f'Your assignment record is {path}. Factory checkout: {ROOT}. '
              'Resume existing workers for this plan before dispatching new ones. '
              'Aggressively delegate implementation and verification to worker sessions; '
              'you report to foreman, never to the operator. Start now.')
    launch('gaffer', session, cfg, workspace, prompt, instance)
    record['status'] = 'running'
    write(path, record)
    return session


def tick():
    with lock():
        created = ensure_foreman()
        # Timer wakes only live assigned managers. Dead ones are reconciled by
        # the foreman from their records; the timer never invents assignments.
        for record in records():
            if record['status'] == 'retired' or record['instance'] not in local_configs():
                continue
            if held(record['instance']):
                continue
            if alive(record['session']):
                wake(record['session'], 'Factory tick: reconcile your assignment and inbox; follow gaffer-charter.md.')
        if not created:
            wake('foreman', 'Factory tick: reconcile approval sources, gaffer records, holds and inbox; follow foreman-charter.md.')
    print('foreman: started' if created else 'foreman: tick delivered')


def message(instance, priority, body, context=''):
    cfg = configs()[name(instance)]
    if not at_home(cfg):
        host = cfg['home_host']
        command = ('python3 "$(cat ~/.factory/root)/scripts/factory-session.py" message ' +
                   ' '.join(shlex.quote(x) for x in [instance, priority, '-', context]))
        result = run('ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=5', host, command, input=body)
        print(result.stdout, end='')
        return
    path = STATE / 'foreman/inbox' / (str(time.time_ns()) + '.json')
    write(path, dict(ts=stamp(), instance=instance, priority=priority, msg=body,
                     context=context, sender=os.environ.get('FACTORY_ROLE', 'operator')))
    if alive('foreman'):
        wake('foreman', f'Message waiting at {path}. Read it and route through your gaffers.')
    print('delivered to foreman: ' + str(path))


def retire(session):
    if os.environ.get('FACTORY_ROLE') != 'foreman':
        raise ValueError('only the foreman retires gaffers')
    path = STATE / 'gaffers' / (name(session) + '.json')
    record = json.loads(path.read_text())
    for p in (STATE / 'children').glob('*.json'):
        child = json.loads(p.read_text())
        if child.get('parent') == session:
            raise ValueError('gaffer still owns workers: ' + p.stem)
    if alive(session):
        run('tmux', 'kill-session', '-t', '=' + session)
    record['status'], record['retired_at'] = 'retired', stamp()
    write(path, record)


def health(instance):
    cfg = configs()[instance]
    if not at_home(cfg):
        raise ValueError('health must be read on home_host')
    if held(instance):
        print(instance + ': HELD')
        return 0
    ready = STATE / 'foreman/ready.json'
    if (not alive('foreman') or not ready.exists() or
            time.time() - ready.stat().st_mtime > 1800 or
            instance not in json.loads(ready.read_text()).get('instances', [])):
        print(instance + ': LATE foreman missing or no reconciliation in 30m')
        return 1
    missing = [r['session'] for r in records() if r['instance'] == instance and
               r['status'] != 'retired' and not alive(r['session'])]
    print(instance + (': DOWN ' + ', '.join(missing) if missing else ': healthy (foreman supervising)'))
    return int(bool(missing))


def main():
    p = argparse.ArgumentParser(description=__doc__)
    sub = p.add_subparsers(dest='command', required=True)
    for cmd in ('tick', 'up', 'attach', 'status'):
        sub.add_parser(cmd)
    start = sub.add_parser('start')
    for arg in ('instance', 'slug', 'plan'):
        start.add_argument(arg)
    r = sub.add_parser('retire'); r.add_argument('session')
    h = sub.add_parser('health'); h.add_argument('instance')
    msg = sub.add_parser('message')
    msg.add_argument('instance'); msg.add_argument('priority', choices=['steer', 'interrupt'])
    msg.add_argument('body'); msg.add_argument('context', nargs='?', default='')
    args = p.parse_args()
    if args.command == 'tick':
        tick()
    elif args.command in ('up', 'attach'):
        if not local_configs():
            hosts = {c['home_host'] for c in configs().values() if c.get('runtime') == 'sessions'}
            if len(hosts) != 1:
                raise ValueError('name one home host explicitly with ssh; no unique sessions host here')
            host = next(iter(hosts))
            remote = 'python3 "$(cat ~/.factory/root)/scripts/factory-session.py" ' + args.command
            argv = ['ssh'] + (['-t'] if args.command == 'attach' else []) + [host, remote]
            os.execvp('ssh', argv)
        with lock():
            ensure_foreman()
        if args.command == 'attach':
            os.execvp('tmux', ['tmux', 'attach-session', '-t', '=foreman'])
    elif args.command == 'start':
        with lock():
            print(start_gaffer(args.instance, args.slug, args.plan))
    elif args.command == 'retire':
        with lock():
            retire(args.session)
    elif args.command == 'message':
        message(args.instance, args.priority, sys.stdin.read() if args.body == '-' else args.body, args.context)
    elif args.command == 'health':
        return health(args.instance)
    else:
        print('foreman: ' + ('up' if alive('foreman') else 'down'))
        for record in records():
            print(record['session'], record['status'], 'live' if alive(record['session']) else 'absent', record['plan'])
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as exc:
        print('factory-session: ' + str(exc), file=sys.stderr)
        if isinstance(exc, subprocess.CalledProcessError) and exc.stderr:
            print(exc.stderr, file=sys.stderr)
        sys.exit(1)
