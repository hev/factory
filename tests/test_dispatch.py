"""Isolated assignment fixtures. Never reads installed config, floor or identity."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, ROOT / 'scripts' / filename)
    obj = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(obj)
    return obj


c = module('dispatch_controller_fixture', 'factory-controller.py')
d = module('dispatch_fixture', 'factory-dispatch.py')


class DispatchTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory(prefix='dispatch-fixture-')
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name).resolve()
        self.state = self.root / 'state'
        self.cfg = dict(name='acme', repo_scope=['acme/app'], worker_harness='codex',
                        worker_model='fixture-model', worker_effort='high')
        self.record = dict(session='gaffer-acme-task', instance='acme', status='running',
                           transport='exec', plan=str(self.root / 'plan.md'))
        for obj, key, value in [(c, 'STATE', self.state), (c, 'BASE', self.state / 'controller'),
                                (c.s, 'STATE', self.state)]:
            self.enterContext(patch.object(obj, key, value))
        self.enterContext(patch.object(c.s, 'local_configs', return_value={'acme': self.cfg}))
        self.enterContext(patch.dict(os.environ, FACTORY_ROLE='gaffer',
            FACTORY_GAFFER_SESSION=self.record['session'], FACTORY_CONTROLLER_TURN='1',
            FACTORY_LEDGER_DIR=str(self.state / 'children'), FACTORY_EVENTS_DIR=str(self.state / 'events'),
            FACTORY_HOLDS_DIR=str(self.state / 'holds')))
        repo = self.root / 'repo'
        self.git('init', str(repo))
        self.git('-C', repo, 'config', 'user.email', 'fixture@example.invalid')
        self.git('-C', repo, 'config', 'user.name', 'Fixture')
        self.git('-C', repo, 'commit', '--allow-empty', '-m', 'fixture')
        self.git('-C', repo, 'remote', 'add', 'origin', 'https://github.com/acme/app.git')
        self.lane = self.root / 'lane'
        self.git('-C', repo, 'worktree', 'add', '-b', 'task', self.lane)
        self.brief = self.root / 'brief.md'; self.brief.write_text('Bounded fixture task and acceptance.')
        self.tasks = [self.task('implement'), self.task('review', 'review', ['implement'])]
        d.save(c, self.record)
        self.live = set()

    def git(self, *args):
        subprocess.run(['git'] + [str(x) for x in args], check=True, capture_output=True)

    def task(self, ident, kind='implementation', after=None):
        return dict(id=ident, kind=kind, after=after or [], repo='acme/app',
                    worktree=str(self.lane), brief=str(self.brief))

    def load(self):
        return c.read(self.state / 'gaffers' / (self.record['session'] + '.json'))

    def commission(self):
        d.commission(c, self.record['session'], self.tasks)
        return self.load()

    def wire(self, session, kind):
        path = self.state / 'events/acme.jsonl'; path.parent.mkdir(parents=True, exist_ok=True)
        with path.open('a') as f:
            f.write(json.dumps(dict(instance='acme', **{'from': session}, kind=kind, text=kind, ts='fixture')) + '\n')

    def fake_run(self, *args, **kwargs):
        if str(args[0]) != 'tmux':
            return self.real_run(*args, **kwargs)
        if args[1] == 'list-sessions':
            return SimpleNamespace(returncode=0, stdout='\n'.join(sorted(self.live)))
        if args[1] == 'has-session':
            return SimpleNamespace(returncode=0 if args[-1].lstrip('=') in self.live else 1, stdout='')
        raise AssertionError('terminal input is forbidden: ' + repr(args))

    def floor(self):
        self.real_run = c.s.run
        self.enterContext(patch.object(c.s, 'run', side_effect=self.fake_run))
        def launched(controller, record, task, cfg):
            d.reservation(controller, record, task, cfg)
            self.live.add(task['session'])
        return self.enterContext(patch.object(d, 'launch', side_effect=launched))

    def test_commission_persists_scope_owner_lanes_and_review(self):
        r = self.commission()
        self.assertEqual(r['owner'], r['session'])
        self.assertEqual(r['repo_scope'], ['acme/app'])
        self.assertEqual(r['worktree_lanes'], {str(self.lane): 'acme/app'})
        self.assertNotEqual(r['tasks'][0]['session'], r['tasks'][1]['session'])
        self.assertEqual(len(list((self.state / 'gaffers').glob('*.json'))), 1)

    def test_rejects_foreign_owner_scope_unreviewed_and_cycle(self):
        with patch.dict(os.environ, FACTORY_GAFFER_SESSION='gaffer-acme-other'):
            with self.assertRaisesRegex(ValueError, 'owning'): self.commission()
        self.tasks[0]['repo'] = 'outside/app'
        with self.assertRaisesRegex(ValueError, 'scope'): self.commission()
        self.tasks[0]['repo'] = 'acme/app'; self.tasks.pop()
        with self.assertRaisesRegex(ValueError, 'review'): self.commission()
        self.tasks[0]['after'] = ['implement']
        with self.assertRaisesRegex(ValueError, 'dependencies'): self.commission()

    def test_lane_rejects_main_checkout_alias_wrong_origin_and_other_plan(self):
        self.tasks[0]['worktree'] = str(self.root / 'repo')
        with self.assertRaisesRegex(ValueError, 'linked'): self.commission()
        alias = self.root / 'alias'; alias.symlink_to(self.lane, target_is_directory=True)
        self.tasks[0]['worktree'] = str(alias)
        with self.assertRaisesRegex(ValueError, 'canonical'): self.commission()
        self.tasks[0]['worktree'] = str(self.lane)
        other = dict(self.record, session='gaffer-acme-other', plan=str(self.root / 'other-plan.md'), worktree_lanes={str(self.lane): 'acme/app'})
        d.save(c, other)
        with self.assertRaisesRegex(ValueError, 'another assignment'): self.commission()

    def test_intermediate_done_dispatches_review_final_done_only_judgment(self):
        r = self.commission(); launched = self.floor()
        d.tend(c, r['session'], self.cfg)
        self.assertEqual(launched.call_count, 1)
        self.wire(r['tasks'][0]['session'], 'done')
        d.tend(c, r['session'], self.cfg)
        self.assertEqual(launched.call_count, 2)
        self.assertEqual(list((c.BASE / 'queues').glob('*/*.json')), [])
        self.wire(r['tasks'][1]['session'], 'done')
        d.tend(c, r['session'], self.cfg)
        events = [c.read(p) for p in (c.BASE / 'queues').glob('*/*.json')]
        self.assertEqual([e['payload']['kind'] for e in events], ['final-done'])
        for _ in range(3): d.tend(c, r['session'], self.cfg)
        self.assertEqual(launched.call_count, 2)
        self.assertEqual(len(list((c.BASE / 'queues').glob('*/*.json'))), 1)

    def test_unchanged_polls_started_notes_and_duplicate_done_do_not_invoke_model(self):
        r = self.commission(); launched = self.floor()
        with patch.object(c, 'execute') as model:
            for _ in range(3): d.tend(c, r['session'], self.cfg)
            for kind in ('started', 'note', 'done', 'done'):
                self.wire(r['tasks'][0]['session'], kind)
                d.tend(c, r['session'], self.cfg)
            model.assert_not_called()
        self.assertEqual(launched.call_count, 2)
        self.assertEqual(list((c.BASE / 'queues').glob('*/*.json')), [])

    def test_blocked_and_failed_are_durable_on_first_observation(self):
        r = self.commission(); launched = self.floor()
        d.tend(c, r['session'], self.cfg)
        for kind in ('blocked', 'failed'):
            self.wire(r['tasks'][0]['session'], kind)
            d.tend(c, r['session'], self.cfg)
        events = [c.read(p) for p in (c.BASE / 'queues').glob('*/*.json')]
        self.assertEqual(len(events), 2)
        self.assertTrue(all(e['payload']['kind'] == 'worker-failed' for e in events))
        self.assertEqual(launched.call_count, 1)
        self.assertEqual(self.load()['tasks'][0]['status'], 'blocked')

    def test_missing_worker_is_one_failure_never_duplicate_launch(self):
        r = self.commission(); launched = self.floor()
        d.tend(c, r['session'], self.cfg)
        self.live.clear()
        for _ in range(3): d.tend(c, r['session'], self.cfg)
        self.assertEqual(launched.call_count, 1)
        self.assertEqual(len(list((c.BASE / 'queues').glob('*/*.json'))), 1)

    def test_crashed_reservation_recovers_same_session_and_ledger(self):
        r = self.commission(); r['tasks'][0]['status'] = 'reserved'; d.save(c, r)
        d.reservation(c, r, r['tasks'][0], self.cfg)
        launched = self.floor(); d.tend(c, r['session'], self.cfg)
        self.assertEqual(launched.call_count, 1)
        self.assertEqual(self.load()['tasks'][0]['session'], r['tasks'][0]['session'])
        self.assertEqual(len(list((self.state / 'children').glob('*.json'))), 1)

    def test_real_boot_receipt_fences_replay_even_after_worker_exits(self):
        path = self.root / 'launch.json'; count = self.root / 'count'
        path.write_text(json.dumps({'command': [sys.executable, '-c',
            'from pathlib import Path; p=Path(' + repr(str(count)) + '); p.write_text(p.read_text()+"x" if p.exists() else "x")']}))
        procs = [subprocess.Popen([sys.executable, str(ROOT / 'scripts/factory-dispatch.py'), 'boot', str(path)]) for _ in range(2)]
        for proc in procs: self.assertEqual(proc.wait(timeout=10), 0)
        self.assertEqual(count.read_text(), 'x')
        self.assertEqual(d.boot(path), 0)
        self.assertEqual(count.read_text(), 'x')

    def test_hold_winddown_pause_and_scope_removal_prevent_launch(self):
        r = self.commission(); launched = self.floor()
        for folder in ('holds', 'winddown'):
            p = self.state / folder / 'acme'; p.parent.mkdir(); p.touch()
            d.tend(c, r['session'], self.cfg); launched.assert_not_called(); p.unlink()
        r = self.load(); r['source_paused'] = True; d.save(c, r)
        d.tend(c, r['session'], self.cfg); launched.assert_not_called()
        r['source_paused'] = False; d.save(c, r)
        self.cfg['repo_scope'] = []
        with self.assertRaisesRegex(ValueError, 'scope'): d.tend(c, r['session'], self.cfg)
        launched.assert_not_called()

    def test_repository_and_global_capacity_include_existing_workers(self):
        r = self.commission(); launched = self.floor()
        for limit, repo in [(2, 'acme/app'), (8, 'acme/other')]:
            self.live.clear()
            for p in (self.state / 'children').glob('*.json'): p.unlink()
            for i in range(limit):
                session = f'worker-acme-existing-{i}'; self.live.add(session)
                c.s.write(self.state / 'children' / (session + '.json'), dict(session=session, repo=repo, parent='other'))
            d.tend(c, r['session'], self.cfg)
            launched.assert_not_called()
            self.assertIn('capacity', self.load()['dispatch_attention'])

    def test_ci_wait_protects_done_then_passed_hands_off_to_review(self):
        r = self.commission(); launched = self.floor()
        d.tend(c, r['session'], self.cfg)
        path = self.state / 'ci/acme/watch.json'
        watch = dict(id='fixture-watch', worker=r['tasks'][0]['session'], state='waiting', ledger={'parent': r['session']})
        c.s.write(path, watch); self.wire(r['tasks'][0]['session'], 'done')
        d.tend(c, r['session'], self.cfg); self.assertEqual(launched.call_count, 1)
        watch['state'] = 'passed'; c.s.write(path, watch)
        original = c.s.run
        def ack(*args, **kwargs):
            if 'ack' in args:
                self.assertIn('fixture-watch', self.load()['tasks'][0]['ci_dispositions'])
                path.unlink(); return SimpleNamespace(returncode=0, stdout='')
            return original(*args, **kwargs)
        with patch.object(c.s, 'run', side_effect=ack): d.tend(c, r['session'], self.cfg)
        self.assertEqual(launched.call_count, 2)
        self.assertEqual(list((c.BASE / 'queues').glob('*/*.json')), [])

    def test_failed_ack_queue_prevents_dispatch_and_preserves_provenance(self):
        r = self.commission(); launched = self.floor()
        p = c.event(r['session'], 'worker-decision-original', {'kind': 'worker-failed', 'reason': 'fixture'})
        e = c.read(p); e.update(status='blocked', attempts=3, run='original-run'); c.s.write(p, e)
        for _ in range(3): d.tend(c, r['session'], self.cfg)
        launched.assert_not_called()
        self.assertEqual(c.read(p), e)
        self.assertIn('judgment', self.load()['dispatch_attention'])

    def test_occupied_terminal_launch_is_rejected_without_input(self):
        r = self.commission(); t = r['tasks'][0]
        calls = []
        def occupied(*args, **kwargs):
            calls.append(args)
            return SimpleNamespace(returncode=1, stdout='')
        with patch.object(c.s, 'run', side_effect=occupied):
            with self.assertRaisesRegex(RuntimeError, 'another launch'):
                d.launch(c, r, t, self.cfg)
        self.assertEqual(calls[0][1:4], ('worker', '--', 'tmux'))
        self.assertIn('--', calls[0])
        self.assertTrue(all('send-keys' not in call and 'capture-pane' not in call for call in calls))
        self.assertEqual(calls[1][1], 'show-environment')

    def test_started_receipt_prevents_launch_after_crash_before_harness(self):
        r = self.commission(); t = r['tasks'][0]
        path = d.reservation(c, r, t, self.cfg)
        (path.parent / 'started.json').write_text('{"pid":1}')
        with patch.object(c.s, 'run') as run:
            d.launch(c, r, t, self.cfg)
            run.assert_not_called()
        t['status'] = 'running'; d.save(c, r)
        self.floor()
        d.tend(c, r['session'], self.cfg)
        self.assertEqual(self.load()['tasks'][0]['status'], 'blocked')
        event = c.read(next((c.BASE / 'queues' / r['session']).glob('*.json')))
        self.assertEqual(event['payload']['reason'], 'worker session disappeared')

    def test_retired_assignment_does_not_launch_and_retains_pending_decision(self):
        r = self.commission(); r['status'] = 'retired'; d.save(c, r)
        p = c.event(r['session'], 'pending-decision', {'kind': 'final-done'})
        launched = self.floor(); d.tend(c, r['session'], self.cfg)
        launched.assert_not_called()
        self.assertEqual(c.read(p)['status'], 'blocked')
        self.assertEqual(c.read(p)['key'], 'pending-decision')

    def test_backlog_reconciliation_preserves_failed_decisions_and_observation_history(self):
        r = self.commission()
        entries = []
        for kind in ('floor-change', 'resync', 'worker-failed', 'final-done', 'steering'):
            p = c.event(r['session'], 'original:' + kind, {'kind': kind})
            e = c.read(p); e.update(status='blocked', attempts=3, run='original-capacity-run')
            c.s.write(p, e); entries.append((p, e))
        d.reconcile_observations(c, r)
        for p, before in entries:
            after = c.read(p)
            self.assertEqual(after['key'], before['key'])
            self.assertEqual(after['attempts'], 3)
            self.assertEqual(after['run'], 'original-capacity-run')
            self.assertEqual(after['payload'], before['payload'])
            expected = 'done' if before['payload']['kind'] in ('floor-change', 'resync') else 'blocked'
            self.assertEqual(after['status'], expected)
        r['status'] = 'retired'
        d.reconcile_observations(c, r)
        decisions = [c.read(p) for p, _ in entries if c.read(p)['status'] != 'done']
        self.assertEqual(len(decisions), 3)
        self.assertTrue(all('unhandled decision' in e['attention'] for e in decisions))

    def test_health_classifies_new_pending_blocked_running_hold_and_retirement(self):
        r = self.commission()
        path = c.event(r['session'], 'decision', {'kind': 'worker-failed'})
        self.assertIn('pending runner', d.queue_health(c, r)[0]['reason'])
        with c.gate(c.BASE / 'locks' / (r['session'] + '.lock')):
            self.assertEqual(d.queue_health(c, r)[0]['status'], 'running')
        e = c.read(path); e['status'] = 'blocked'; c.s.write(path, e)
        self.assertIn('acknowledgment', d.queue_health(c, r)[0]['reason'])
        hold = self.state / 'holds/acme'; hold.parent.mkdir(); hold.touch()
        self.assertEqual(d.queue_health(c, r)[0]['reason'], 'factory held')
        hold.unlink(); r['source_paused'] = True
        self.assertEqual(d.queue_health(c, r)[0]['reason'], 'source paused')
        r['status'] = 'retired'
        self.assertIn('unhandled decision', d.queue_health(c, r)[0]['reason'])

    def test_worker_options_and_brief_preserve_configured_harness(self):
        r = self.commission(); path = d.reservation(c, r, r['tasks'][0], self.cfg)
        cmd = c.read(path)['command']
        self.assertEqual(cmd[:5], ['codex', '-a', 'never', '-s', 'danger-full-access'])
        self.assertIn('fixture-model', cmd)
        self.assertIn('model_reasoning_effort="high"', cmd)
        brief = (path.parent / 'brief.md').read_text()
        self.assertIn('directed ONLY by ' + r['session'], brief)
        self.assertIn('ci wait acme ' + r['tasks'][0]['session'], brief)
        with self.assertRaisesRegex(ValueError, 'unsupported'):
            d.worker_command({'worker_harness': 'unknown'}, str(self.lane), str(self.brief))


if __name__ == '__main__': unittest.main()
