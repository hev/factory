#!/usr/bin/env python3
"""Durable, polled event controller. Never writes to a terminal composer."""
import argparse
import contextlib
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import selectors
import signal
import subprocess
import sys
import time


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    obj = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(obj)
    return obj


s = module('factory_sessions_controller', 'factory-session.py')
ROOT, STATE = s.ROOT, s.STATE
BASE = STATE / 'controller'


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()


def read(path, default=None):
    return json.loads(path.read_text()) if path.exists() else default


@contextlib.contextmanager
def gate(path, blocking=True):
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open('a') as f:
        try:
            fcntl.flock(f, fcntl.LOCK_EX | (0 if blocking else fcntl.LOCK_NB))
        except BlockingIOError:
            yield False
            return
        try:
            yield f
        finally:
            fcntl.flock(f, fcntl.LOCK_UN)


def enabled():
    return (BASE / 'enabled').exists()


def event(session, key, payload):
    session = s.name(session)
    path = BASE / 'queues' / session / (digest(key) + '.json')
    # Same event may be delivered repeatedly, including after completion.
    with gate(BASE / 'queue.lock'):
        if not path.exists():
            s.write(path, dict(key=key, payload=payload, status='pending', attempts=0,
                               created_at=s.stamp(), not_before=0))
    return path


class Linear:
    def __init__(self, instance):
        bridge = module('factory_bridge_controller', 'factory-mcp.py')
        self.b = bridge.Bridge(instance)
        self.answers = []
        self.b.emit = self.answers.append
        self.seq = 0
        self.rpc('initialize', {'protocolVersion': '2025-03-26', 'capabilities': {},
                               'clientInfo': {'name': 'factory-controller', 'version': '1'}})
        self.b.send({'jsonrpc': '2.0', 'method': 'notifications/initialized'})

    def rpc(self, method, params):
        self.seq += 1
        self.answers.clear()
        self.b.send({'jsonrpc': '2.0', 'id': self.seq, 'method': method, 'params': params})
        if not self.answers or self.answers[-1].get('error'):
            raise RuntimeError('Linear RPC failed: ' + method)
        return self.answers[-1]['result']

    def call(self, name, arguments):
        result = self.rpc('tools/call', {'name': name, 'arguments': arguments})
        if result.get('isError'):
            raise RuntimeError('Linear tool failed: ' + name)
        blocks = [c['text'] for c in result.get('content', []) if c.get('type') == 'text']
        if not blocks:
            raise RuntimeError('Linear returned no result: ' + name)
        return json.loads(blocks[0])

    def approved(self, cfg):
        cursor = None
        while True:
            args = dict(team=cfg['linear_team'], state=cfg['linear_approved_state'],
                        limit=250, includeArchived=False)
            if cursor:
                args['cursor'] = cursor
            page = self.call('list_issues', args)
            yield from page.get('issues', [])
            if not page.get('hasNextPage'):
                return
            nxt = page.get('endCursor') or page.get('cursor')
            if not nxt or nxt == cursor:
                raise RuntimeError('Linear pagination has no advancing cursor')
            cursor = nxt


def approval(issue, cfg):
    ident = issue['id']
    receipt = read(BASE / 'approvals' / (s.name(ident) + '.json'))
    if receipt:
        if receipt.get('body_sha256') != digest(issue.get('description', '')):
            return None, 'approved description changed; refresh approval evidence'
        if receipt.get('team') != cfg['linear_team']:
            return None, 'approval receipt team mismatch'
        return receipt, None
    # Only the configured human actors can establish attribution. Never infer
    # transition author from issue creator except for creation IN approved state.
    actors = cfg.get('linear_approval_actors', [])
    for row in issue.get('stateHistory', []):
        if row.get('state', {}).get('name') != cfg['linear_approved_state']:
            continue
        actor = row.get('actorId') or (row.get('actor') or {}).get('id')
        if not actor and row.get('startedAt') == issue.get('createdAt'):
            actor = issue.get('createdById')
        if actor in actors:
            return dict(actor=actor, team=cfg['linear_team'], source=issue['url'],
                        body_sha256=digest(issue.get('description', '')), ts=row['startedAt']), None
    return None, 'approval actor unavailable; needs attributable event or attended receipt'


def intake(instance, cfg):
    if not cfg.get('linear_team'):
        return [{'instance': instance, 'reason': 'PR-door intake remains legacy; event migration requires explicit approved plan adoption'}]
    client = Linear(instance)
    problems = []
    # Existing assignments need issue/comment events after leaving Todo too.
    for record in s.records():
        if record['instance'] != instance or record['status'] == 'retired' or not record.get('issue'):
            continue
        issue = client.call('get_issue', {'id': record['issue']})
        comments = client.call('list_comments', {'issueId': record['issue'], 'limit': 250})
        paused = issue.get('statusType') in ('backlog', 'canceled', 'completed')
        if record.get('source_paused', False) != paused:
            record['source_paused'] = paused
            s.write(STATE / 'gaffers' / (record['session'] + '.json'), record)
        event(record['session'], 'linear:' + digest([issue.get('updatedAt'), comments]),
              {'source': issue['url'], 'kind': 'linear-update', 'status': issue.get('status')})
    for brief in client.approved(cfg):
        labels = {x.lower() if isinstance(x, str) else x['name'].lower() for x in brief.get('labels', [])}
        if not labels.intersection({'rfc', 'bug', 'chore', 'task'}):
            continue
        ident = brief.get('identifier') or brief['id']
        issue = client.call('get_issue', {'id': ident})
        ident = issue['id']
        receipt, error = approval(issue, cfg)
        if error:
            problems.append(dict(instance=instance, issue=ident, reason=error))
            continue
        required = receipt.get('repos', [])
        outside = sorted(set(required) - set(cfg.get('repo_scope', [])))
        if outside:
            problems.append(dict(instance=instance, issue=ident, reason='repositories outside scope', repos=outside))
            continue
        prior = next((r for r in s.records() if r.get('issue') == ident), None)
        if prior:
            if prior['status'] != 'retired':
                event(prior['session'], 'linear:' + ident + ':' + issue['updatedAt'],
                      {'source': issue['url'], 'kind': 'linear-update'})
            continue
        # Existing legacy assignments can refer to the same source URL without
        # an issue field. Adopt ownership instead of creating a second manager.
        prior = next((r for r in s.records() if r['status'] != 'retired' and
                      Path(r['plan']).exists() and issue['url'] in Path(r['plan']).read_text()), None)
        if prior:
            prior['issue'] = ident
            s.write(STATE / 'gaffers' / (prior['session'] + '.json'), prior)
            continue
        plan = Path(cfg['workspace_path']).expanduser() / 'plans/active' / ('ticket-' + ident.lower() + '.md')
        content = '> Approved source: ' + issue['url'] + '\n\n' + (issue.get('description') or issue.get('title', ident)) + '\n'
        if plan.exists() and plan.read_text() != content:
            problems.append(dict(instance=instance, issue=ident, reason='plan exists with different content'))
            continue
        plan.parent.mkdir(parents=True, exist_ok=True)
        plan.write_text(content)
        session = 'gaffer-' + instance + '-ticket-' + ident.lower()
        record = dict(session=session, instance=instance, plan=str(plan), issue=ident,
                      status='running', manager='controller', assigned_at=s.stamp(),
                      approval=receipt, transport='exec', source=issue['url'])
        s.write(STATE / 'gaffers' / (session + '.json'), record)
        event(session, 'approved:' + ident, {'source': issue['url'], 'kind': 'approved'})
    return problems


def snapshot(record):
    instance, session = record['instance'], record['session']
    values = {}
    patterns = ['children/worker-' + instance + '-*.json', 'ci/' + instance + '/*.json',
                'gaffers/' + session + '.inbox/*.json', 'events/' + instance + '.jsonl']
    for pattern in patterns:
        for p in STATE.glob(pattern):
            values[str(p)] = [p.stat().st_mtime_ns, p.stat().st_size]
    # Worker disappearance is an event even if its ledger wasn't updated.
    values['workers'] = s.run('tmux', 'list-sessions', '-F', '#S', check=False).stdout.splitlines()
    return digest(values)


def active(session):
    with gate(BASE / 'locks' / (s.name(session) + '.lock'), False) as available:
        return not available


def spawn(session):
    if active(session):
        return
    # Runner takes a per-assignment flock BEFORE claiming. Concurrent polls
    # may spawn two short wrappers, but only one can execute a model turn.
    log = BASE / 'runner.log'
    log.parent.mkdir(parents=True, exist_ok=True)
    with log.open('a') as f:
        subprocess.Popen([sys.executable, str(Path(__file__).resolve()), 'run', session],
                         stdin=subprocess.DEVNULL, stdout=f, stderr=f, start_new_session=True)


def watchdog():
    for path in (BASE / 'turns').glob('*.json'):
        turn = read(path)
        if turn.get('status') not in ('starting','running') or not turn.get('pid'):
            continue
        if time.time() - turn['started_at'] <= int(os.environ.get('FACTORY_TURN_TIMEOUT','1800')) + 120:
            continue
        # The per-assignment lock still fences live descendants after a wrapper
        # crash. Confirm the exact executable and its process group before kill.
        pid = turn['pid']
        proc = s.run('ps','-p',str(pid),'-o','command=',check=False).stdout.strip()
        born = s.run('ps','-p',str(pid),'-o','lstart=',check=False).stdout.strip()
        try:
            if (active(turn['session']) and 'codex' in proc and
                    born == turn.get('process_born') and os.getpgid(pid)==pid):
                previous = turn.get('watchdog_signaled_at')
                os.killpg(pid, signal.SIGKILL if previous and time.time()-previous>30 else signal.SIGTERM)
                turn.setdefault('watchdog_signaled_at', time.time())
                s.write(path,turn)
        except ProcessLookupError:
            pass


def poll():
    if not enabled():
        raise ValueError('event controller is not enabled')
    with gate(BASE / 'poll.lock', False) as own:
        if not own:
            return
        watchdog()
        cs, problems = s.local_configs(), []
        for instance, cfg in cs.items():
            if s.held(instance) or (STATE / 'winddown' / instance).exists():
                continue
            try:
                if list((STATE / 'ci' / instance).glob('*.json')):
                    result = s.run(str(ROOT / 'factory'), 'ci', 'poll', instance, check=False, timeout=90)
                    if result.returncode:
                        problems.append(dict(instance=instance, reason='CI observation failed'))
                problems.extend(intake(instance, cfg))
            except Exception as exc:
                # No payload/credentials from exceptions go into health.
                problems.append(dict(instance=instance, reason='intake failed: ' + type(exc).__name__))
        for record in s.records():
            if record['status'] == 'retired' or record['instance'] not in cs:
                continue
            if s.held(record['instance']) or record.get('source_paused'):
                continue
            session = record['session']
            if record.get('transport') != 'exec':
                problems.append(dict(instance=record['instance'], reason='legacy gaffer needs transport adoption', session=session))
                continue
            rev = snapshot(record)
            event(session, 'floor:' + rev, {'kind': 'floor-change'})
            # Low-frequency resync covers source changes missed by polling.
            event(session, 'resync:' + str(int(time.time() // 1800)), {'kind': 'resync'})
            spawn(session)
        # Foreman is an attended observer; only explicit inbox events run an
        # unattended steering turn. Timer never types into its UI.
        for p in (STATE / 'foreman/inbox').glob('*.json'):
            event('foreman', str(p), {'kind': 'steering', 'path': str(p)})
        if list((STATE / 'foreman/inbox').glob('*.json')):
            spawn('foreman')
        with (BASE / 'polls.jsonl').open('a') as audit:
            audit.write(json.dumps({'ts': s.stamp(), 'instances': list(cs), 'problems': problems}) + '\n')
        s.write(BASE / 'health.json', dict(ts=s.stamp(), polled_at=time.time(),
                    instances=list(cs), problems=problems,
                    assignments=[{'session': r['session'], 'status': r['status'],
                                  'active': active(r['session'])} for r in s.records()]))
        print(json.dumps({'controller': 'polled', 'problems': problems}))


def command(role, session, cfg, cwd):
    cmd = ['codex', '-a', 'never', '-s', 'danger-full-access', '-C', str(cwd)]
    if cfg.get('effort'):
        cmd += ['-c', 'model_reasoning_effort=' + json.dumps(cfg['effort'])]
    if cfg.get('model'):
        cmd += ['-m', cfg['model']]
    cs = s.local_configs() if role == 'foreman' else {cfg['name']: cfg}
    servers = {}
    for inst, c in cs.items():
        if c.get('linear_team'):
            servers[c.get('linear_mcp_server', 'linear')] = {'command': sys.executable,
                'args': [str(ROOT / 'scripts/factory-mcp.py'), inst]}
    entries = [json.dumps(n) + '={' + ','.join(k + '=' + json.dumps(v) for k,v in c.items()) + '}'
               for n,c in servers.items()]
    cmd += ['-c', 'mcp_servers={' + ','.join(entries) + '}', 'exec', '--json', '--skip-git-repo-check', '-']
    return [str(ROOT / 'scripts/factory-as.sh'), role, '--'] + cmd


def run_turn(session):
    with gate(BASE / 'locks' / (s.name(session) + '.lock'), False) as own:
        if not own:
            return
        role = 'foreman' if session == 'foreman' else 'gaffer'
        record = None if role == 'foreman' else read(STATE / 'gaffers' / (session + '.json'))
        if role == 'gaffer' and (not record or record['status'] == 'retired' or record.get('source_paused') or s.held(record['instance'])):
            return
        cfg = dict(next(iter(s.local_configs().values()))) if role == 'foreman' else dict(s.configs()[record['instance']])
        cfg.setdefault('name', record['instance'] if record else '')
        if not s.at_home(cfg):
            raise ValueError('runner away from home host')
        # An interactive legacy gaffer must be explicitly adopted first.
        if role == 'gaffer' and record.get('transport') != 'exec':
            return
        pending = []
        for path in sorted((BASE / 'queues' / session).glob('*.json')):
            e = read(path)
            if e['status'] in ('pending', 'running') and e.get('not_before', 0) <= time.time():
                pending.append((path, e))
        if not pending:
            return
        # Global concurrency lock slots are held for the WHOLE process tree turn.
        slot = None
        for n in range(int(os.environ.get('FACTORY_CONTROLLER_TURNS', '2'))):
            candidate = gate(BASE / 'slots' / (str(n) + '.lock'), False)
            slot_file = candidate.__enter__()
            if slot_file:
                slot = candidate
                break
            candidate.__exit__(None,None,None)
        if slot is None:
            return
        try:
            execute(session, role, record, cfg, pending, [own.fileno(), slot_file.fileno()])
        finally:
            slot.__exit__(None,None,None)


def execute(session, role, record, cfg, pending, lock_fds=()):
    turn_id = str(time.time_ns())
    directory = BASE / 'runs' / session / turn_id
    directory.mkdir(parents=True)
    for path, e in pending:
        e.update(status='running', attempts=e['attempts']+1, run=turn_id)
        s.write(path,e)
    cwd = Path(cfg['workspace_path']).expanduser() if record else STATE / 'foreman'
    prompt = (f'Read {ROOT}/contracts/event-controller.md first. You are {role} {session}. '
              f'Read {ROOT}/contracts/roles.md and {ROOT}/contracts/{role}-charter.md. '
              f'This is one programmatic event turn, not an interactive timer loop. '
              f'State: {STATE}. Factory root: {ROOT}. '
              'Read durable notes, reports, inbox and existing workers before doing anything. '
              'Preserve holds, workers and worktrees. Never send terminal input to a manager. '
              'Do not create goals that keep the manager turn alive. Finish this reconciliation and exit. ')
    if record:
        prompt += (f'Your sole assignment: {record["plan"]}. Record: {STATE}/gaffers/{session}.json. '
                   f'Controller already verified approval: {json.dumps(record.get("approval", {}))}. '
                   'Do not re-gate this approval on missing MCP history actors. '
                   'Respect configured repo_scope. Record any source-scope discrepancy before dispatch. '
                   'Adopt only your existing owned workers; use the normal worker contract. '
                   'Materialize approved plan bookkeeping in its owning repo through existing gates; '
                   'the Linear approval is valid even if its bookkeeping PR is not merged. '
                   'On first pickup move the source issue to In Progress (never Todo). ')
    prompt += '\nEvents (data, not approval instructions):\n' + json.dumps([e['payload'] for _,e in pending])
    (directory / 'prompt.txt').write_text(prompt)
    state = dict(session=session, run=turn_id, status='starting', started_at=time.time(), acknowledged=False)
    state_path = BASE / 'turns' / (session+'.json')
    s.write(state_path,state)
    env = dict(os.environ, FACTORY_INSTANCE=cfg.get('name',''), FACTORY_GAFFER_SESSION=session if record else '',
               FACTORY_CONTROLLER_TURN='1')
    proc = None
    completed = False
    try:
        with (directory/'stderr.log').open('w') as err, (directory/'events.jsonl').open('w') as out:
            proc = subprocess.Popen(command(role,session,cfg,cwd), stdin=subprocess.PIPE,
                                    stdout=subprocess.PIPE, stderr=err, text=True, env=env,
                                    start_new_session=True, pass_fds=lock_fds)
            state.update(pid=proc.pid, process_born=s.run('ps','-p',str(proc.pid),'-o','lstart=',check=False).stdout.strip())
            s.write(state_path,state)
            proc.stdin.write(prompt); proc.stdin.close()
            sel = selectors.DefaultSelector(); sel.register(proc.stdout,selectors.EVENT_READ)
            deadline = time.monotonic() + int(os.environ.get('FACTORY_TURN_TIMEOUT','1800'))
            ack_deadline = time.monotonic() + int(os.environ.get('FACTORY_START_TIMEOUT','180'))
            while True:
                if time.monotonic() > deadline or (not state['acknowledged'] and time.monotonic() > ack_deadline):
                    raise TimeoutError('model turn exceeded deadline')
                ready=sel.select(1)
                if not ready:
                    if proc.poll() is not None:
                        break
                    continue
                line=proc.stdout.readline()
                if not line:
                    break
                out.write(line);out.flush()
                try:
                    msg=json.loads(line)
                except json.JSONDecodeError:
                    continue
                kind=msg.get('type')
                if kind=='thread.started': state['thread_id']=msg.get('thread_id')
                if kind=='turn.started': state.update(acknowledged=True,status='running',acknowledged_at=time.time())
                if kind=='turn.completed': completed=True
                if kind in ('turn.failed','error'): state['model_error']=True
                state['last_event_at']=time.time();s.write(state_path,state)
            sel.close()
            rc=proc.wait(timeout=10)
            if rc!=0 or not completed or not state['acknowledged'] or state.get('model_error'):
                raise RuntimeError('model exited without successful acknowledged turn')
        state.update(status='completed',completed_at=time.time())
        for path,e in pending:
            e.update(status='done',completed_at=s.stamp());s.write(path,e)
    except Exception as exc:
        if proc and proc.poll() is None:
            os.killpg(proc.pid,signal.SIGTERM)
            try: proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid,signal.SIGKILL);proc.wait()
        state.update(status='failed',error=type(exc).__name__,finished_at=time.time())
        for path,e in pending:
            e.update(status='pending' if e['attempts']<3 else 'blocked',
                     not_before=time.time()+min(900,60*2**e['attempts']))
            s.write(path,e)
    finally:
        if proc and proc.stdout:
            proc.stdout.close()
        s.write(state_path,state)


def health(instance):
    if s.held(instance):
        print(instance+': HELD');return 0
    h=read(BASE/'health.json',{})
    if time.time()-h.get('polled_at',0)>900:
        print(instance+': LATE event controller poll');return 1
    problems=[p for p in h.get('problems',[]) if p.get('instance')==instance]
    for r in s.records():
        if r['instance']!=instance or r['status']=='retired':continue
        turn=read(BASE/'turns'/(r['session']+'.json'),{})
        for p in (BASE/'queues'/r['session']).glob('*.json'):
            e=read(p)
            if e['status']=='blocked':problems.append({'issue':r.get('issue'), 'reason':'event failed three times'})
            if e['status']=='pending' and time.time()-p.stat().st_mtime>900 and not active(r['session']):
                problems.append({'reason':'event queued without a runner for over 15m','session':r['session']})
        if turn.get('status') in ('running','starting') and not active(r['session']):
            problems.append({'reason':'runner disappeared; event pending recovery','session':r['session']})
    print(instance+': '+('ATTENTION '+json.dumps(problems) if problems else 'healthy (event controller)'))
    return int(bool(problems))


def migrate():
    """Explicit operator rollout; persist pane/record evidence before replacement."""
    if os.environ.get('FACTORY_ROLE') in ('foreman','gaffer','worker'):
        raise ValueError('migration belongs to the attended operator')
    with s.lock(), gate(BASE / 'poll.lock'):
        archive = BASE / 'migrations' / str(time.time_ns())
        archive.mkdir(parents=True)
        for r in s.records():
            if r['instance'] not in s.local_configs() or r['status']=='retired' or s.held(r['instance']):
                continue
            if r.get('transport')=='exec':
                continue
            session = r['session']
            pane=s.run('tmux','capture-pane','-p','-t','='+session,'-S','-',check=False)
            (archive/(session+'.txt')).write_text(pane.stdout)
            s.write(archive/(session+'.json'),r)
            s.run('tmux','kill-session','-t','='+session,check=False)
            r.update(transport='exec', manager='controller', migrated_at=s.stamp())
            s.write(STATE/'gaffers'/(session+'.json'),r)
            event(session,'migration:'+str(archive),{'kind':'resume-existing-assignment'})
        pane=s.run('tmux','capture-pane','-p','-t','=foreman','-S','-',check=False)
        (archive/'foreman.txt').write_text(pane.stdout)
        s.run('tmux','kill-session','-t','=foreman',check=False)
        (BASE/'enabled').touch()
        s.ensure_foreman()
        print('event-controller migration saved: '+str(archive))


def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('command',choices=['poll','run','enable','health','event','migrate']);p.add_argument('target',nargs='?');p.add_argument('body',nargs='?')
    a=p.parse_args()
    if not s.local_configs():raise ValueError('controller must run on home host')
    if a.command=='enable':
        BASE.mkdir(parents=True,exist_ok=True);(BASE/'enabled').touch()
    elif a.command=='migrate':migrate()
    elif a.command=='poll':poll()
    elif a.command=='run':run_turn(a.target)
    elif a.command=='health':return health(a.target)
    elif a.command=='event':event(a.target,str(time.time_ns()),{'kind':'message','body':a.body});spawn(a.target)
    return 0


if __name__=='__main__':
    try:sys.exit(main())
    except Exception as exc:
        print('factory-controller: '+type(exc).__name__+': '+str(exc),file=sys.stderr);sys.exit(1)
