"""Isolated eight-slot poll -> real shell harvest -> commissioned launch.

No installed state, credentials, manager models or production terminals.
"""
import contextlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import unittest
from unittest.mock import patch

import test_dispatch as fixture

c, d = fixture.c, fixture.d
proof = fixture.module('legacy_completion_fixture', 'factory-worker-completion.py')
ROOT = Path(__file__).resolve().parents[1]


class LegacyLifecycleTest(unittest.TestCase):
    git = fixture.DispatchTest.git
    task = fixture.DispatchTest.task
    load = fixture.DispatchTest.load
    commission = fixture.DispatchTest.commission

    def setUp(self):
        fixture.DispatchTest.setUp(self)
        self.cfg.update(runtime='sessions', home_host='fixture', workspace_path=str(self.root))
        self.enterContext(patch.object(c, 'dispatch', d))
        self.enterContext(patch.object(c.s, 'configs', return_value={'acme': self.cfg}))
        self.enterContext(patch.dict(os.environ, FACTORY_HOSTNAME_OVERRIDE='fixture', FACTORY_STATE_DIR=str(self.state)))
        self.enterContext(patch.object(c, 'intake', return_value=[]))
        self.trace_output = sys.stdout
        self.output = io.StringIO()
        self.enterContext(contextlib.redirect_stdout(self.output))
        c.BASE.mkdir(parents=True, exist_ok=True); (c.BASE / 'enabled').touch()
        self.spawn = self.enterContext(patch.object(c, 'spawn', side_effect=AssertionError('unexpected manager call')))
        self.install = self.root / 'install'
        scripts = self.install / 'scripts'; scripts.mkdir(parents=True)
        reaper = scripts / 'factory-reap.sh'
        reaper.write_text((ROOT / 'scripts/factory-reap.sh').read_text().replace(
            'export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"', '# fixture PATH'))
        reaper.chmod(0o755)
        shutil.copy(ROOT / 'scripts/factory-worker-completion.py', scripts)
        # The real cleanup implementation records the linked shared worktree;
        # dispatch must defer sweeping it while the legacy assignment exists.
        shutil.copy(ROOT / 'scripts/factory-clean-worktrees.py', scripts)
        configs = self.install / 'factories'; configs.mkdir()
        (configs / 'acme.toml').write_text('runtime="sessions"\nrepo_scope=["acme/app"]\n')
        self.floor = self.root / 'floor.json'; self.floor.write_text('{}')
        bins = self.root / 'bin'; bins.mkdir(); (bins / 'python3').symlink_to(sys.executable)
        terminal = '''import json,os,sys,time
from pathlib import Path
p=Path(os.environ['FIXTURE_FLOOR']);floor=json.loads(p.read_text());a=sys.argv[1:]
def name():return a[a.index('-t')+1].lstrip('=')
if a[0]=='list-sessions':
 for n,v in floor.items():
  print(n if a[-1]=='#S' else n+'|'+str(int(time.time()) if v.get('redraw') else 0)+'|'+str(v.get('attached',0)))
elif a[0]=='has-session':sys.exit(0 if name() in floor else 1)
elif a[0]=='show-environment':print('FACTORY_TASK_LAUNCH='+floor[name()].get('identity',''))
elif a[0]=='capture-pane':print(floor[name()].get('pane','idle harness prompt'))
elif a[0]=='display-message':print(os.environ['FIXTURE_LANE'] if a[-1]=='#{pane_current_path}' else '123')
elif a[0]=='kill-session':
 del floor[name()];p.write_text(json.dumps(floor))
else:raise AssertionError(a)
'''
        path = bins / 'tmux'; path.write_text('#!' + sys.executable + '\n' + terminal); path.chmod(0o755)
        self.enterContext(patch.dict(os.environ, PATH=str(bins) + ':' + os.environ['PATH'],
            FIXTURE_FLOOR=str(self.floor), FIXTURE_LANE=str(self.lane)))
        run = c.s.run
        def fixture_run(*args, **kwargs):
            if str(args[0]).endswith('/scripts/factory-reap.sh'):
                args = (reaper, *args[1:])
            if len(args) > 2 and str(args[0]).endswith('/factory') and args[1:3] == ('ci', 'poll'):
                return subprocess.CompletedProcess(args, 0, '', '')
            return run(*args, **kwargs)
        self.enterContext(patch.object(c.s, 'run', side_effect=fixture_run))
        def launched(controller, record, task, cfg):
            d.reservation(controller, record, task, cfg)
            floor = json.loads(self.floor.read_text()); floor[task['session']] = {'pane': 'esc to interrupt'}
            self.floor.write_text(json.dumps(floor))
        self.launch = self.enterContext(patch.object(d, 'launch', side_effect=launched))

    def child(self, name, **extra):
        child = dict(session='worker-acme-' + name, instance='acme', parent='gaffer-acme-aaa-legacy',
                     repo='acme/app', plan='legacy', dispatched_at='2026-01-01T00:00:00Z', worktree=str(self.lane))
        child.update(extra)
        c.s.write(self.state / 'children' / (child['session'] + '.json'), child)
        return child

    def test_eight_slots_reconcile_without_tasks_or_repeated_judgment(self):
        # New work is approved/commissioned in a different lane. Legacy workers
        # share the original linked lane and must keep it and all evidence.
        legacy_lane = self.lane
        self.lane = self.root / 'next-lane'
        self.git('-C', self.root / 'repo', 'worktree', 'add', '-b', 'next', self.lane)
        self.tasks = [self.task('implement'), self.task('review', 'review', ['implement'])]
        commissioned = self.commission()
        self.lane = legacy_lane
        legacy = dict(self.record, session='gaffer-acme-aaa-legacy', plan=str(self.root / 'legacy.md'),
                      tasks=[{'step': 'descriptive checklist', 'status': 'done'}],
                      worktree_lanes=[str(self.lane)])
        d.save(c, legacy)
        names = ('review', 'implementation', 'ci', 'attached', 'working', 'ambiguous', 'credit', 'identity')
        children = {n: self.child(n) for n in names}
        # Legacy workers in other repositories consume global slots, leaving
        # the unchanged two-per-repository cap free for the commissioned lane.
        for n in names[2:]:
            children[n]['repo'] = 'acme/other'
            c.s.write(self.state / 'children' / (children[n]['session'] + '.json'), children[n])
        (self.install / 'factories/acme.toml').write_text('runtime="sessions"\nrepo_scope=["acme/app","acme/other"]\n')
        children['implementation']['pr'] = 41
        children['ambiguous']['pr'] = 42
        children['identity']['launch_identity'] = '/expected/launch.json'
        for n in ('ambiguous', 'identity'):
            c.s.write(self.state / 'children' / (children[n]['session'] + '.json'), children[n])
        floor = {v['session']: {} for v in children.values()}
        floor[children['review']['session']] = {'redraw': True}
        floor[children['attached']['session']] = {'attached': 1}
        floor[children['working']['session']] = {'pane': 'esc to interrupt', 'redraw': True}
        floor[children['credit']['session']] = {'pane': 'Insufficient credits'}
        floor[children['identity']['session']] = {'identity': '/different/launch.json'}
        self.floor.write_text(json.dumps(floor))
        watch = self.state / 'ci/acme/wait.json'
        c.s.write(watch, dict(instance='acme', worker=children['ci']['session'], state='passed'))
        evidence = self.state / 'evidence/acme' / children['review']['session'] / 'review.md'
        evidence.parent.mkdir(parents=True); evidence.write_text('Independent review findings and exact reviewed head.')
        c.poll()
        self.launch.assert_not_called()
        self.assertEqual(len(json.loads(self.floor.read_text())), 8)
        self.assertIn('capacity', self.load()['dispatch_attention'])
        print('fixture filled: ' + json.dumps(sorted(floor)), file=self.trace_output)
        spool = self.state / 'events/acme.jsonl'; spool.parent.mkdir(exist_ok=True)
        # Durable done is sufficient for a reviewer with no PR/completed_at.
        spool.write_text(json.dumps(dict(instance='acme', **{'from': children['review']['session']},
                                        ts='2026-01-02T00:00:00Z', kind='done', text=str(evidence))) + '\n')
        for n in names[1:]:
            children[n]['completed_at'] = '2026-01-02T00:00:00Z'
            if n == 'ambiguous': children[n].pop('completed_at')
            c.s.write(self.state / 'children' / (children[n]['session'] + '.json'), children[n])
        c.poll()
        self.assertEqual(self.launch.call_count, 1)
        remaining = json.loads(self.floor.read_text())
        self.assertEqual(set(remaining), {children[n]['session'] for n in names[2:]} | {commissioned['tasks'][0]['session']})
        self.assertEqual(len(remaining), 7)
        print('fixture next poll: ' + json.dumps(dict(released=[children[n]['session'] for n in names[:2]],
              retained=[children[n]['session'] for n in names[2:]], launched=commissioned['tasks'][0]['session'],
              global_occupied=len(remaining), global_cap=8, repo_cap=2)), file=self.trace_output)
        self.assertTrue(watch.exists()); self.assertEqual(evidence.read_text(), 'Independent review findings and exact reviewed head.')
        self.assertTrue((self.lane / '.git').is_file())
        saved = c.read(self.state / 'gaffers' / (legacy['session'] + '.json'))
        self.assertEqual(saved['tasks'], legacy['tasks']); self.assertEqual(saved['worktree_lanes'], legacy['worktree_lanes'])
        for n in names[:2]:
            log = self.state / 'harvest/acme' / (children[n]['session'] + '.log')
            self.assertIn('evidence:', log.read_text()); self.assertIn('# ledger:', log.read_text())
            self.assertFalse((self.state / 'children' / (children[n]['session'] + '.json')).exists())
        self.assertTrue(list((self.state / 'harvest/acme/worktrees').rglob('*.json')))
        for _ in range(3): c.poll()
        self.assertEqual(self.launch.call_count, 1); self.spawn.assert_not_called()
        self.assertEqual(c.health('acme'), 1)
        self.assertIn(legacy['session'], self.output.getvalue())
        self.assertIn('no automatic retry', self.output.getvalue())
        self.assertIn('handle and acknowledge', self.output.getvalue())
        print('fixture replay: three quiet polls; launches=1; manager_calls=0; review evidence, shared worktree, checklist and lane ownership preserved', file=self.trace_output)

    def test_empty_legacy_and_holds_preserve_structure_and_authority(self):
        child = self.child('finished', parent=self.record['session'], completed_at='2026-01-02T00:00:00Z')
        self.floor.write_text(json.dumps({child['session']: {'redraw': True}}))
        hold = self.state / 'holds/acme'; hold.parent.mkdir(); hold.touch()
        c.poll()
        self.assertIn(child['session'], json.loads(self.floor.read_text()))
        hold.unlink()
        record = self.load(); record['source_paused'] = True; d.save(c, record)
        c.poll()
        self.assertIn(child['session'], json.loads(self.floor.read_text()))
        record['source_paused'] = False; d.save(c, record)
        winddown = self.state / 'winddown/acme'; winddown.parent.mkdir(); winddown.touch()
        c.poll()
        self.assertEqual(json.loads(self.floor.read_text()), {})
        self.assertNotIn('tasks', self.load())
        self.assertNotIn('commissions', self.load())
        self.launch.assert_not_called(); self.spawn.assert_not_called()

    def test_malformed_executable_history_is_not_a_legacy_checklist(self):
        with self.assertRaisesRegex(ValueError, 'malformed commissioned'):
            d.executable_tasks(dict(tasks=[{'session': 'worker-acme-reserved', 'status': 'reserved'}]))
        record = dict(self.record, tasks=[{'step': 'review findings', 'status': 'done'}])
        d.save(c, record)
        with fixture.decision_context(record['session']):
            with self.assertRaisesRegex(ValueError, 'attended archival'):
                d.commission(c, record['session'], self.tasks)
        self.assertEqual(self.load()['tasks'], record['tasks'])

    def test_completion_requires_current_unambiguous_testimony(self):
        child = self.child('proof')
        spool = self.root / 'wire.jsonl'
        def event(kind, ts='2026-01-02T00:00:00Z', **extra):
            return dict(instance='acme', **{'from': child['session']}, kind=kind, ts=ts, text='durable evidence', **extra)
        for events, pane, expected in [
            ([], '', False), ([event('done')], '', True),
            ([event('pr')], '', False), ([event('done', '2025-01-01T00:00:00Z')], '', False),
            ([event('done'), event('started')], '', False),
            ([event('done'), event('blocked')], '', False),
            ([event('done'), event('failed')], '', False),
            ([event('done')], 'Esc to interrupt', False),
            ([event('done')], 'You’ve hit your limit', False),
            ([event('done', 'bad-date')], '', False),
        ]:
            with self.subTest(events=events, pane=pane):
                spool.write_text(''.join(json.dumps(e) + '\n' for e in events))
                self.assertEqual(proof.completion(child, 'acme', child['parent'], spool, pane)[0], expected)
        self.assertFalse(proof.completion(child, 'acme', 'gaffer-acme-other', spool, '')[0])
        spool.write_text('{corrupt}\n')
        with self.assertRaisesRegex(ValueError, 'invalid completed'):
            proof.completion(child, 'acme', child['parent'], spool, '')


if __name__ == '__main__': unittest.main()
