"""Controller/dispatcher integration on temporary repositories and state only."""
import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch

import test_dispatch as fixture

c, d = fixture.c, fixture.d


class ControllerIntegrationTest(unittest.TestCase):
    git = fixture.DispatchTest.git
    task = fixture.DispatchTest.task
    load = fixture.DispatchTest.load
    commission = fixture.DispatchTest.commission
    wire = fixture.DispatchTest.wire
    fake_run = fixture.DispatchTest.fake_run
    floor = fixture.DispatchTest.floor

    def setUp(self):
        fixture.DispatchTest.setUp(self)
        self.cfg.update(runtime='sessions', home_host='fixture', workspace_path=str(self.root))
        c.BASE.mkdir(parents=True, exist_ok=True); (c.BASE / 'enabled').touch()
        self.enterContext(patch.object(c, 'dispatch', d))
        self.enterContext(patch.object(c.s, 'configs', return_value={'acme': self.cfg}))
        self.enterContext(patch.dict(os.environ, FACTORY_HOSTNAME_OVERRIDE='fixture'))
        self.enterContext(patch.object(c, 'intake', return_value=[]))
        self.enterContext(contextlib.redirect_stdout(io.StringIO()))
        self.launch = self.floor()
        self.command = self.enterContext(patch.object(c, 'command', return_value=[sys.executable, '-c',
            """import sys,json,os,hashlib
from pathlib import Path
prompt=sys.stdin.read()
print('{"type":"turn.started"}')
if '"kind": "final-done"' in prompt:
    state=Path(os.environ['FACTORY_STATE_DIR'])
    path=state/'gaffers'/(os.environ['FACTORY_GAFFER_SESSION']+'.json')
    record=json.loads(path.read_text())
    proof=state/'acceptance.md';proof.write_text('Fixture independent review passed; contract merge awaits operator.')
    record['delivery']={'event':os.environ['FACTORY_CONTROLLER_EVENT'],'status':'awaiting-gate','evidence':str(proof),
        'evidence_sha256':hashlib.sha256(json.dumps(proof.read_text(),sort_keys=True).encode()).hexdigest()}
    path.write_text(json.dumps(record))
print('{"type":"turn.completed"}')
"""]))
        self.spawn = self.enterContext(patch.object(c, 'spawn', side_effect=c.run_turn))

    def receipts(self):
        return [c.read(p) for p in (c.BASE / 'runs' / self.record['session']).glob('*/receipt.json')]

    def test_commission_to_delivery_and_three_quiet_polls(self):
        # Commission CLI is exercised as the sole owning acknowledged turn.
        tasks = self.root / 'tasks.json'; tasks.write_text(json.dumps(self.tasks))
        with patch.object(sys, 'argv', ['controller', 'commission', self.record['session'], str(tasks)]):
            c.main()
        commission = c.event(self.record['session'], 'approved:fixture', {'kind': 'approved'})
        c.poll()
        self.assertEqual(c.read(commission)['status'], 'done')
        self.assertEqual(len(self.receipts()), 1)
        c.poll()
        self.assertEqual(self.launch.call_count, 1)
        for _ in range(3): c.poll()
        self.assertEqual(self.command.call_count, 1)
        r = self.load()
        self.wire(r['tasks'][0]['session'], 'done'); c.poll()
        self.assertEqual(self.launch.call_count, 2)
        self.assertEqual(self.command.call_count, 1)
        self.wire(r['tasks'][1]['session'], 'done'); c.poll()
        self.assertEqual(self.command.call_count, 2)
        receipts = self.receipts()
        self.assertTrue(all(r['status'] == 'completed' and r['event_key'] for r in receipts))
        self.assertEqual(len(self.load()['model_turns']), 2)
        for receipt in receipts:
            self.assertEqual(c.read(Path(receipt['event_path']))['key'], receipt['event_key'])
        final = next(r for r in receipts if r['event_key'].startswith('final:'))
        prompt = c.BASE / 'runs' / r['session'] / final['run'] / 'prompt.txt'
        self.assertIn('verify all acceptance criteria, independent review and exact-head CI', prompt.read_text())
        self.assertIn('existing output gates', prompt.read_text())
        for _ in range(3): c.poll()
        self.assertEqual(self.command.call_count, 2)
        self.assertEqual(self.launch.call_count, 2)

    def test_blocked_and_failed_invoke_judgment_on_next_poll(self):
        r = self.commission(); c.poll()
        for kind in ('blocked', 'failed'):
            self.wire(r['tasks'][0]['session'], kind)
            before = self.command.call_count
            c.poll()
            self.assertEqual(self.command.call_count, before + 1)
        self.assertEqual(self.launch.call_count, 1)
        self.assertTrue(all(r['event_key'].startswith('wire:') for r in self.receipts()))

    def test_failed_ack_is_not_retried_and_steering_preserves_disposition(self):
        self.commission()
        old = c.event(self.record['session'], 'first-decision', {'kind': 'steering'})
        self.command.return_value = [sys.executable, '-c', 'import sys;sys.stdin.read()']
        c.poll()
        failed = c.read(old)
        self.assertEqual(failed['status'], 'blocked')
        for _ in range(3): c.poll()
        self.assertEqual(self.command.call_count, 1)
        self.assertEqual(self.launch.call_count, 0)
        self.assertEqual(c.health('acme'), 1)
        fresh = c.event(self.record['session'], 'recovery-steering', {'kind': 'steering'})
        entry = c.read(fresh); entry['status'] = 'running'; c.s.write(fresh, entry)
        with patch.dict(os.environ, FACTORY_CONTROLLER_EVENT='recovery-steering'):
            c.resolve_event(self.record['session'], 'first-decision', 'Inspected side effects; no worker started.')
        after = c.read(old)
        self.assertEqual(after['status'], 'done')
        self.assertEqual(after['run'], failed['run'])
        self.assertEqual(after['attempts'], failed['attempts'])
        self.assertEqual(after['payload'], failed['payload'])
        self.assertEqual(after['disposition']['event'], 'recovery-steering')

    def test_historical_observations_reconcile_but_retired_decisions_remain(self):
        r = self.commission()
        for kind in ('floor-change', 'resync', 'worker-failed'):
            p = c.event(r['session'], 'old:' + kind, {'kind': kind})
            e = c.read(p); e.update(status='blocked', attempts=3, run='capacity-outage'); c.s.write(p, e)
        r['status'] = 'retired'; d.save(c, r)
        for _ in range(3): c.poll()
        self.command.assert_not_called(); self.launch.assert_not_called()
        rows = [e for _, e in c.pending_events(r['session'])]
        self.assertEqual(sum(e['status'] == 'done' for e in rows), 2)
        unresolved = next(e for e in rows if e['status'] == 'blocked')
        self.assertEqual(unresolved['run'], 'capacity-outage')
        self.assertEqual(unresolved['attempts'], 3)
        self.assertIn('retired', unresolved['attention'])
        self.assertEqual(c.health('acme'), 1)

    def test_held_decision_health_and_winddown_allow_existing_completion(self):
        r = self.commission(); c.poll()
        hold = self.state / 'holds/acme'; hold.parent.mkdir(); hold.touch()
        self.wire(r['tasks'][0]['session'], 'blocked'); c.poll()
        self.command.assert_not_called()
        self.assertEqual(c.health('acme'), 1)
        hold.unlink()
        winddown = self.state / 'winddown/acme'; winddown.parent.mkdir(); winddown.touch()
        c.poll()
        self.assertEqual(self.command.call_count, 1)
        self.assertEqual(self.launch.call_count, 1)

    def test_manager_capacity_retains_event_and_health_attention(self):
        r = self.commission()
        p = c.event(r['session'], 'decision', {'kind': 'steering'})
        with patch.dict(os.environ, FACTORY_CONTROLLER_TURNS='1'), c.gate(c.BASE / 'slots/0.lock'):
            c.poll()
        self.command.assert_not_called()
        self.assertEqual(c.read(p)['attempts'], 0)
        self.assertEqual(c.health('acme'), 1)
        c.poll()
        self.assertEqual(self.command.call_count, 1)

    def test_final_success_without_evidence_is_an_incomplete_decision(self):
        r = self.commission()
        for t in r['tasks']: t.update(status='done', done_claim='fixture')
        d.save(c, r)
        self.command.return_value = [sys.executable, '-c',
            'import sys;sys.stdin.read();print(\'{"type":"turn.started"}\');print(\'{"type":"turn.completed"}\')']
        c.poll()
        self.assertEqual(self.receipts()[0]['status'], 'failed')
        self.assertEqual(self.receipts()[0]['error'], 'ValueError')
        self.assertIn('incomplete judgment output',c.pending_events(r['session'])[0][1]['attention'])
        for _ in range(3): c.poll()
        self.assertEqual(self.command.call_count, 1)
        self.assertEqual(c.health('acme'), 1)

    def test_parallel_assignments_share_repository_capacity_atomically(self):
        from concurrent.futures import ThreadPoolExecutor
        records=[]
        for index in range(3):
            record=dict(self.record,session=f'gaffer-acme-plan-{index}',plan=str(self.root/f'plan-{index}.md'))
            lane=self.root/f'lane-{index}'
            self.git('-C',self.root/'repo','worktree','add','-b',f'branch-{index}',lane)
            d.save(c,record)
            tasks=[dict(task,worktree=str(lane)) for task in self.tasks]
            with patch.dict(os.environ,FACTORY_GAFFER_SESSION=record['session']):
                d.commission(c,record['session'],tasks)
            records.append(record)
        with ThreadPoolExecutor(max_workers=3) as pool:
            futures=[pool.submit(d.tend,c,r['session'],self.cfg) for r in records]
            for future in futures: future.result(timeout=10)
        self.assertEqual(self.launch.call_count,2)
        stored=[c.read(self.state/'gaffers'/(r['session']+'.json')) for r in records]
        self.assertEqual(sum(t['status']=='running' for r in stored for t in r['tasks']),2)
        self.assertEqual(sum('capacity' in r.get('dispatch_attention','') for r in stored),1)

    def test_reaper_failure_does_not_delay_an_existing_blocked_decision(self):
        r=self.commission();c.poll()
        self.wire(r['tasks'][0]['session'],'blocked')
        with patch.object(d,'reap',side_effect=RuntimeError('fixture cleanup unavailable')):
            c.poll()
        self.assertEqual(self.command.call_count,1)
        self.assertEqual(c.health('acme'),1)
        self.assertEqual(self.launch.call_count,1)

    def test_long_plan_releases_completed_capacity_through_scoped_harvest(self):
        self.tasks.extend([self.task('implement-two',after=['review']),
                           self.task('review-two','review',['implement-two'])])
        r=self.commission()
        def harvested(controller,record):
            for task in record['tasks']:
                if task['status']=='done':
                    child=d.ledger_dir(c)/(task['session']+'.json')
                    if child.exists():
                        self.assertIn('completed_at',c.read(child))
                        child.unlink()
                    self.live.discard(task['session'])
        with patch.object(d,'reap',side_effect=harvested):
            for task in r['tasks']:
                c.poll()
                self.assertIn(task['session'],self.live)
                self.assertLessEqual(len(self.live),2)
                self.wire(task['session'],'done')
            c.poll()
        self.assertEqual(self.launch.call_count,4)
        self.assertEqual(self.command.call_count,1)
        self.assertEqual(self.load()['delivery']['status'],'awaiting-gate')

    def test_closed_source_still_allows_delivered_cleanup_but_no_dispatch(self):
        r=self.commission()
        for t in r['tasks']:t.update(status='done',done_claim='fixture')
        r.update(source_paused=True,delivery={'status':'delivered'})
        d.save(c,r)
        with patch.object(d,'reap') as reap:
            c.poll();reap.assert_called_once()
        self.launch.assert_not_called();self.command.assert_not_called()

    def test_partial_model_output_cannot_defeat_ack_deadline(self):
        r = self.commission()
        p = c.event(r['session'], 'partial-output', {'kind': 'steering'})
        self.command.return_value = [sys.executable, '-c',
            'import sys,time;sys.stdin.read();sys.stdout.write("{");sys.stdout.flush();time.sleep(30)']
        with patch.dict(os.environ, FACTORY_START_TIMEOUT='1'):
            c.poll()
        self.assertEqual(c.read(p)['status'], 'blocked')
        self.assertEqual(self.receipts()[0]['error'], 'TimeoutError')

    def test_assignment_lock_prevents_dispatch_and_model_concurrently(self):
        r = self.commission()
        c.event(r['session'], 'commission', {'kind': 'approved'})
        with c.gate(c.BASE / 'locks' / (r['session'] + '.lock')):
            c.poll()
        self.launch.assert_not_called(); self.command.assert_not_called()
        c.poll()
        self.assertEqual(self.command.call_count, 1)

    def test_blocked_task_requires_durable_owner_decision_and_new_attempt(self):
        r = self.commission(); c.poll()
        self.wire(r['tasks'][0]['session'], 'failed'); c.poll()
        with self.assertRaisesRegex(ValueError, 'running durable'):
            d.resolve_task(c, r['session'], 'implement', 'replacement follows')
        p = c.event(r['session'], 'recovery', {'kind': 'steering'})
        e = c.read(p); e['status'] = 'running'; c.s.write(p, e)
        with patch.dict(os.environ, FACTORY_CONTROLLER_EVENT='recovery'):
            d.resolve_task(c, r['session'], 'implement', 'Inspected attempt; retry is required.')
        self.assertEqual(self.load()['tasks'][0]['disposition']['event'], 'recovery')
        self.tasks.insert(1, self.task('retry', after=['implement']))
        self.tasks[-1]['after'] = ['implement', 'retry']
        self.commission()
        self.assertEqual(self.load()['tasks'][0]['status'], 'done')
        self.assertNotEqual(self.load()['tasks'][0]['session'], self.load()['tasks'][1]['session'])


if __name__ == '__main__': unittest.main()
