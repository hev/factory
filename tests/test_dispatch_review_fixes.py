"""Independent-review regressions using real CLI/reaper and disposable state."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import test_dispatch as fixture

c, d = fixture.c, fixture.d
ROOT = Path(__file__).resolve().parents[1]


class DispatchReviewFixTest(unittest.TestCase):
    def setUp(self):
        self.f = fixture.DispatchTest()
        self.f.setUp()
        self.addCleanup(self.f.doCleanups)
        self.install = self.f.root / 'install'
        scripts = self.install / 'scripts'; scripts.mkdir(parents=True)
        configs = self.install / 'factories'; configs.mkdir()
        for name in ('factory-controller.py', 'factory-dispatch.py', 'factory-session.py'):
            shutil.copy2(ROOT / 'scripts' / name, scripts / name)
        (configs / 'acme.toml').write_text('runtime="sessions"\nhome_host="fixture"\nrepo_scope=["acme/app"]\nidle_minutes="1"\n')
        self.tasks = self.f.root / 'tasks.json'; self.tasks.write_text(json.dumps(self.f.tasks))
        self.cli = [sys.executable, str(scripts / 'factory-controller.py'), 'commission',
                    self.f.record['session'], str(self.tasks)]
        self.env = dict(os.environ, FACTORY_STATE_DIR=str(self.f.state),
                        FACTORY_HOSTNAME_OVERRIDE='fixture', PYTHONDONTWRITEBYTECODE='1')

    def invoke(self, env):
        return subprocess.run(self.cli, env=env, text=True, capture_output=True, timeout=15)

    def test_commission_cli_rejects_invalid_event_and_turn_without_mutation(self):
        session = self.f.record['session']
        with fixture.decision_context(session):
            env = dict(self.env, FACTORY_CONTROLLER_EVENT=os.environ['FACTORY_CONTROLLER_EVENT'],
                       FACTORY_CONTROLLER_RUN=os.environ['FACTORY_CONTROLLER_RUN'])
            event_path = c.BASE / 'queues' / session / (c.digest(env['FACTORY_CONTROLLER_EVENT']) + '.json')
            turn_path = c.BASE / 'turns' / (session + '.json')
            receipt_path = c.BASE / 'runs' / session / env['FACTORY_CONTROLLER_RUN'] / 'receipt.json'
            originals = {p: c.read(p) for p in (event_path, turn_path, receipt_path)}
            before = self.f.load()
            for case in ('no-event', 'done', 'blocked', 'pending', 'foreign-event', 'no-run',
                         'stale-run', 'foreign-owner', 'foreign-turn', 'unacknowledged',
                         'finished-turn', 'wrong-event-path', 'wrong-receipt'):
                with self.subTest(case=case):
                    for p, value in originals.items(): c.s.write(p, value)
                    attempt = dict(env)
                    if case == 'no-event': attempt.pop('FACTORY_CONTROLLER_EVENT', None)
                    elif case in ('done', 'blocked', 'pending'):
                        c.s.write(event_path, dict(originals[event_path], status=case))
                    elif case == 'foreign-event':
                        key = 'foreign-approval'
                        foreign = c.event('gaffer-acme-other', key, {'kind': 'approved'})
                        c.s.write(foreign, dict(c.read(foreign), status='running', run=env['FACTORY_CONTROLLER_RUN']))
                        attempt['FACTORY_CONTROLLER_EVENT'] = key
                    elif case == 'no-run': attempt.pop('FACTORY_CONTROLLER_RUN', None)
                    elif case == 'stale-run': attempt['FACTORY_CONTROLLER_RUN'] = 'old-run'
                    elif case == 'foreign-owner': attempt['FACTORY_GAFFER_SESSION'] = 'gaffer-acme-other'
                    elif case == 'foreign-turn': c.s.write(turn_path, dict(originals[turn_path], session='gaffer-acme-other'))
                    elif case == 'unacknowledged': c.s.write(turn_path, dict(originals[turn_path], acknowledged=False))
                    elif case == 'finished-turn': c.s.write(turn_path, dict(originals[turn_path], status='completed'))
                    elif case == 'wrong-event-path': c.s.write(turn_path, dict(originals[turn_path], event_path='/foreign/event'))
                    elif case == 'wrong-receipt': c.s.write(receipt_path, dict(originals[receipt_path], run='old-run'))
                    result = self.invoke(attempt)
                    self.assertNotEqual(result.returncode, 0, result.stdout)
                    self.assertEqual(self.f.load(), before)

    def test_real_acknowledged_event_turn_commissions_cli_idempotently(self):
        session = self.f.record['session']
        self.f.cfg.update(workspace_path=str(self.f.root))
        path = c.event(session, 'approved:real-cli', {'kind': 'approved', 'source': 'immutable fixture'})
        runner = self.f.root / 'runner.py'
        runner.write_text('''import json,os,subprocess,sys,time
from pathlib import Path
sys.stdin.read()
print('{"type":"turn.started"}', flush=True)
state=Path(os.environ['FACTORY_STATE_DIR'])/'controller'
session=os.environ['FACTORY_GAFFER_SESSION']; run=os.environ['FACTORY_CONTROLLER_RUN']
receipt=state/'runs'/session/run/'receipt.json'
deadline=time.monotonic()+5
while not json.loads(receipt.read_text()).get('acknowledged'):
    if time.monotonic()>deadline: raise RuntimeError('acknowledgment not recorded')
    time.sleep(.01)
for _ in range(2): subprocess.run(sys.argv[1:],check=True)
print('{"type":"turn.completed"}', flush=True)
''')
        with patch.dict(os.environ, self.env), patch.object(c, 'command', return_value=[sys.executable, str(runner)] + self.cli):
            c.execute(session, 'gaffer', self.f.load(), self.f.cfg, [(path, c.read(path))])
        event = c.read(path)
        self.assertEqual(event['status'], 'done')
        self.assertEqual(event['attempts'], 1)
        self.assertEqual(event['payload']['source'], 'immutable fixture')
        record = self.f.load()
        self.assertEqual(len(record['tasks']), 2)
        self.assertEqual(len(record['commissions']), 1)
        self.assertEqual(record['commissions'][0]['event'], event['key'])
        self.assertEqual(record['commissions'][0]['run'], event['run'])
        receipt = c.read(c.BASE / 'runs' / session / event['run'] / 'receipt.json')
        self.assertEqual(receipt['status'], 'completed')
        self.assertEqual(receipt['event_path'], str(path))
        self.assertEqual(record['model_turns'][0]['run'], event['run'])
        # A process retaining a formerly valid environment cannot amend after exit.
        result = self.invoke(dict(self.env, FACTORY_CONTROLLER_EVENT=event['key'], FACTORY_CONTROLLER_RUN=event['run']))
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.f.load(), record)

    def test_collision_through_real_reaper_preserves_unowned_terminal_and_ledger(self):
        record = self.f.commission(); task = record['tasks'][0]
        child = self.f.state / 'children' / (task['session'] + '.json')
        scripts = self.install / 'scripts'
        reaper = scripts / 'factory-reap.sh'
        reaper.write_text((ROOT / 'scripts/factory-reap.sh').read_text().replace(
            'export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"', '# isolated fixture PATH'))
        (scripts / 'factory-worker-completion.py').write_text((ROOT / 'scripts/factory-worker-completion.py').read_text())
        (scripts / 'factory-clean-worktrees.py').write_text('import sys;sys.exit(0)\n')
        bins = self.f.root / 'bin'; bins.mkdir(); (bins / 'python3').symlink_to(sys.executable)
        marker = self.f.root / 'killed'
        stub = '''import sys,os
from pathlib import Path
args=sys.argv[1:];session=os.environ['COLLISION_SESSION']
if args[0]=='list-sessions':print(session+'|0|0')
elif args[0]=='has-session':sys.exit(0 if not Path(os.environ['KILLED']).exists() else 1)
elif args[0]=='show-environment':print('FACTORY_TASK_LAUNCH='+os.environ['TERMINAL_IDENTITY'])
elif args[0]=='display-message':print('/fixture/lane' if args[-1]=='#{pane_current_path}' else '123')
elif args[0]=='capture-pane':print('UNRELATED TERMINAL')
elif args[0]=='kill-session':Path(os.environ['KILLED']).touch()
else:raise AssertionError(args)
'''
        for name, body in [('tmux', stub), ('pgrep', 'import sys;sys.exit(1)\n')]:
            p = bins / name; p.write_text('#!' + sys.executable + '\n' + body); p.chmod(0o755)
        env = dict(self.env, PATH=str(bins) + ':' + os.environ['PATH'],
                   FACTORY_GAFFER_SESSION=record['session'], FACTORY_DEFER_WORKTREE_CLEANUP='1',
                   COLLISION_SESSION=task['session'], KILLED=str(marker), TERMINAL_IDENTITY='foreign-launch')
        for case in ('absent-ledger', 'foreign-ledger', 'reserved-before-crash', 'old-unverified-ledger'):
            with self.subTest(case=case):
                child.unlink(missing_ok=True)
                if case == 'foreign-ledger':
                    c.s.write(child, dict(session=task['session'], instance='acme', parent='gaffer-acme-other', task_id='other'))
                elif case in ('reserved-before-crash', 'old-unverified-ledger'):
                    d.reservation(c, record, task, self.f.cfg)
                    if case == 'old-unverified-ledger':
                        value = c.read(child); value.pop('launch_identity'); c.s.write(child, value)
                before = child.read_bytes() if child.exists() else None
                with patch.object(c.s, 'run', return_value=SimpleNamespace(returncode=1, stdout='', stderr='collision')):
                    with self.assertRaises((RuntimeError, ValueError)):
                        d.launch(c, record, task, self.f.cfg)
                reserved = child.read_bytes()
                if before is not None: self.assertEqual(reserved, before)
                result = subprocess.run(['bash', str(reaper), 'acme'], env=env, text=True, capture_output=True, timeout=15)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertFalse(marker.exists(), result.stdout)
                self.assertEqual(child.read_bytes(), reserved)
                self.assertFalse((self.f.state / 'harvest/acme' / (task['session'] + '.log')).exists())
        # Matching launch identity alone is not completion. Explicit testimony
        # makes the recovered terminal harvestable.
        child.unlink(); launch = d.reservation(c, record, task, self.f.cfg)
        with patch.object(c.s, 'run', side_effect=[SimpleNamespace(returncode=1),
                SimpleNamespace(returncode=0, stdout='FACTORY_TASK_LAUNCH=' + str(launch))]):
            d.launch(c, record, task, self.f.cfg)
        value = c.read(child); value['completed_at'] = c.s.stamp(); c.s.write(child, value)
        result = subprocess.run(['bash', str(reaper), 'acme'], env=dict(env, TERMINAL_IDENTITY=str(launch)),
                                text=True, capture_output=True, timeout=15)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(marker.exists(), result.stdout)
        self.assertFalse(child.exists())


if __name__ == '__main__': unittest.main()
