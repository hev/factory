"""FAC-35 acceptance uses only temporary state and controlled kernel locks."""
import contextlib
import io
import os
import unittest
from datetime import datetime, timezone
from unittest.mock import patch

import test_controller

c = test_controller.c


class FairAdmissionTest(unittest.TestCase):
    setUp = test_controller.ControllerTest.setUp
    fake = test_controller.ControllerTest.fake

    def assignment(self, name, age=60, **fields):
        r = dict(session='gaffer-acme-' + name, instance='acme', status='running',
                 transport='exec', plan=str(self.root / 'plan.md'), **fields)
        c.s.write(self.state / 'gaffers' / (r['session'] + '.json'), r)
        p = self.enqueue(r['session'], 'first', age)
        return r, p

    def enqueue(self, session, key, age):
        p = c.event(session, key, {'kind': 'steering'})
        e = c.read(p)
        e['created_at'] = datetime.fromtimestamp(c.time.time() - age, timezone.utc).isoformat()
        c.s.write(p, e)
        return p

    def admission(self, r):
        return c.read(self.state / 'gaffers' / (r['session'] + '.json'))['controller_admission']

    def success(self):
        return self.fake('import sys;sys.stdin.read();print(\'{"type":"turn.started"}\');print(\'{"type":"turn.completed"}\')')

    def test_refill_admits_next_without_another_poll(self):
        first, p = self.assignment('first', age=200)
        second, q = self.assignment('second', age=100)
        spawned = []
        with patch.dict(os.environ, FACTORY_CONTROLLER_TURNS='1'), self.success(), patch.object(c, 'spawn', side_effect=spawned.append):
            c.run_turn(first['session'], refill=True)
            self.assertEqual(spawned, [second['session']])
            c.run_turn(spawned.pop(), refill=True)
            self.assertEqual(spawned, [])
        self.assertEqual(c.read(p)['status'], 'done')
        self.assertEqual(c.read(q)['status'], 'done')

    def test_refill_uses_second_slot_without_waiting_for_first_to_finish(self):
        first, _ = self.assignment('first', age=200)
        second, _ = self.assignment('second', age=100)
        spawned = []
        def execute(*args):
            self.assertEqual(spawned, [second['session']])
        with patch.dict(os.environ, FACTORY_CONTROLLER_TURNS='2'), patch.object(c, 'spawn', side_effect=spawned.append), patch.object(c, 'execute', side_effect=execute):
            c.run_turn(first['session'], refill=True)

    def test_capacity_deferral_does_not_spawn_a_retry_chain(self):
        first, p = self.assignment('first')
        with c.gate(self.base/'slots/0.lock'), patch.dict(os.environ, FACTORY_CONTROLLER_TURNS='1'), patch.object(c,'spawn') as spawn:
            c.run_turn(first['session'], refill=True)
            spawn.assert_not_called()
        self.assertEqual(c.read(p)['attempts'],0)

    def test_dispatch_wake_runs_after_owner_and_manager_slot_release(self):
        first, path = self.assignment('first')
        def wake():
            self.assertFalse(c.active(first['session']))
            with c.gate(self.base / 'slots/0.lock', False) as slot:
                self.assertTrue(slot)
            self.assertEqual(c.read(path)['status'], 'done')
        with patch.dict(os.environ, FACTORY_CONTROLLER_TURNS='1'), self.success(), patch.object(c, 'spawn'), patch.object(c, 'wake_dispatch', side_effect=wake) as dispatch:
            c.run_turn(first['session'], refill=True)
            dispatch.assert_called_once()

    def test_deferred_turn_does_not_generate_dispatch_wake(self):
        first, _ = self.assignment('first')
        with c.gate(self.base / 'slots/0.lock'), patch.dict(os.environ, FACTORY_CONTROLLER_TURNS='1'), patch.object(c, 'wake_dispatch') as dispatch:
            c.run_turn(first['session'], refill=True)
            dispatch.assert_not_called()

    def test_saturation_is_logged_durable_and_not_a_model_attempt(self):
        r, p = self.assignment('waiting')
        before = c.read(p)
        with c.gate(self.base / 'slots/0.lock'), c.gate(self.base / 'slots/1.lock'), patch.object(c, 'execute') as execute:
            c.run_turn(r['session'])
            c.run_turn(r['session'])
            execute.assert_not_called()
        self.assertEqual(c.read(p), before)
        state = self.admission(r)
        self.assertEqual(state['deferrals'], 2)
        self.assertTrue(state['last_deferred_at'])
        log = (self.base / 'runner.log').read_text()
        self.assertEqual(log.count(r['session']), 2)
        self.assertIn('all global slots occupied', log)
        with self.success():
            c.run_turn(r['session'])
        self.assertEqual(c.read(p)['status'], 'done')
        self.assertEqual(c.read(p)['attempts'], 1)
        self.assertEqual(self.admission(r)['deferrals'], 2)

    def test_busy_backlog_yields_to_every_waiter_across_polls(self):
        busy, _ = self.assignment('a-busy', age=300)
        waiters = [self.assignment(name, age=200 - n)[0] for n, name in enumerate(('b', 'c', 'd'))]
        for n in range(8):
            self.enqueue(busy['session'], 'backlog-' + str(n), 300)
        (self.base / 'enabled').touch()
        served = []
        real_execute = c.execute

        def execute(session, *args):
            served.append(session)
            return real_execute(session, *args)

        def spawn(session):
            count = len(served)
            c.run_turn(session)
            if len(served) > count:
                # Model occupancy lasts for the rest of this simulated poll.
                occupied.enter_context(c.gate(self.base / 'slots/0.lock'))

        # Fixed poll order favors busy; durable admission must override it.
        with patch.dict(os.environ, FACTORY_CONTROLLER_TURNS='1'), self.success(), patch.object(c, 'execute', side_effect=execute), patch.object(c, 'intake', return_value=[]), patch.object(c.dispatch, 'tend'), patch.object(c, 'spawn', side_effect=spawn), contextlib.redirect_stdout(io.StringIO()):
            for _ in range(4):
                with contextlib.ExitStack() as occupied:
                    c.poll()
        self.assertEqual(served[:4], [busy['session']] + [r['session'] for r in waiters])
        self.assertGreaterEqual(self.admission(busy)['deferrals'], 1)

    def test_direct_run_cannot_jump_older_waiter_or_foreman(self):
        newer, p = self.assignment('new', age=20)
        older, _ = self.assignment('old', age=100)
        self.enqueue('foreman', 'observe', 200)
        with self.success():
            c.run_turn(newer['session'])
            self.assertEqual(c.read(p)['attempts'], 0)
            c.run_turn(older['session'])
            self.assertFalse((self.base / 'admission/foreman.json').exists())
            c.run_turn('foreman')
            c.run_turn(older['session'])
            c.run_turn(newer['session'])
        self.assertEqual(c.read(p)['status'], 'done')
        self.assertIn('last_admitted_at', c.read(self.base / 'admission/foreman.json'))

    def test_ineligible_older_queues_do_not_block(self):
        for reason in ('held', 'paused', 'retired', 'legacy', 'backoff', 'attempted', 'unknown', 'active', 'remote'):
            with self.subTest(reason=reason):
                older, p = self.assignment('old-' + reason, age=300)
                path = self.state / 'gaffers' / (older['session'] + '.json')
                e = c.read(p)
                if reason == 'paused': older['source_paused'] = True
                if reason == 'retired': older['status'] = 'retired'
                if reason == 'legacy': older['transport'] = 'tmux'
                if reason in ('held', 'remote'): older['instance'] = reason
                if reason == 'backoff': e['not_before'] = c.time.time() + 1000
                if reason == 'attempted': e['attempts'] = 1
                if reason == 'unknown': e['payload']['kind'] = 'unexpected'
                c.s.write(path, older)
                c.s.write(p, e)
                newer, target = self.assignment('new-' + reason, age=10)
                configs = {'acme': self.cfg, 'held': self.cfg}
                with contextlib.ExitStack() as stack:
                    stack.enter_context(patch.object(c.s, 'local_configs', return_value=configs))
                    stack.enter_context(patch.object(c.s, 'held', side_effect=lambda inst: inst == 'held'))
                    if reason == 'active': stack.enter_context(c.gate(self.base / 'locks' / (older['session'] + '.lock')))
                    stack.enter_context(self.success())
                    c.run_turn(newer['session'])
                self.assertEqual(c.read(target)['status'], 'done')
                # Retire fixture so it cannot interfere with subsequent subtests.
                older['status'] = 'retired'
                c.s.write(path, older)

    def test_crash_before_claim_is_retryable_but_attempt_failure_is_not(self):
        r, p = self.assignment('crash')
        with patch.object(c, 'execute', side_effect=RuntimeError('wrapper crash')):
            with self.assertRaisesRegex(RuntimeError, 'wrapper crash'):
                c.run_turn(r['session'])
        self.assertEqual(c.read(p)['attempts'], 0)
        self.assertFalse(c.active(r['session']))
        with self.fake('import sys;sys.stdin.read()'):
            c.run_turn(r['session'])
        self.assertEqual(c.read(p)['status'], 'blocked')
        with patch.object(c, 'execute') as execute:
            c.run_turn(r['session'])
            execute.assert_not_called()
        recovery = self.enqueue(r['session'], 'explicit-recovery', 0)
        with self.success():
            c.run_turn(r['session'])
        self.assertEqual(c.read(recovery)['status'], 'done')
        self.assertEqual(c.read(p)['status'], 'blocked')

    def test_fences_held_but_admission_released_during_execution(self):
        r, _ = self.assignment('locks')
        def execute(session, role, record, cfg, pending, fds):
            self.assertTrue(c.active(session))
            self.assertEqual(len(fds), 2)
            for fd in fds: os.fstat(fd)
            with c.gate(self.base / 'slots/0.lock', False) as slot:
                self.assertFalse(slot)
            with c.gate(self.base / 'admission.lock', False) as admission:
                self.assertTrue(admission)
        with patch.dict(os.environ, FACTORY_CONTROLLER_TURNS='1'), patch.object(c, 'execute', side_effect=execute):
            c.run_turn(r['session'])
        with c.gate(self.base / 'slots/0.lock', False) as slot:
            self.assertTrue(slot)

    def test_poll_health_reports_aged_pending_including_hold_and_backoff(self):
        r, p = self.assignment('aged', age=901)
        e = c.read(p); e['not_before'] = c.time.time() + 1000; c.s.write(p, e)
        self.assignment('fresh', age=10)
        done = self.enqueue(r['session'], 'done', 5000)
        e = c.read(done); e['status'] = 'done'; c.s.write(done, e)
        (self.base / 'enabled').touch()
        with patch.object(c.s, 'held', return_value=True), patch.object(c, 'intake', return_value=[]), patch.object(c.dispatch, 'tend'), patch.object(c, 'spawn'), contextlib.redirect_stdout(io.StringIO()):
            c.poll()
        h = c.read(self.base / 'health.json')
        self.assertEqual(len(h['pending_ages']), 2)
        self.assertEqual(len(h['problems']), 1)
        problem = h['problems'][0]
        self.assertEqual(problem['session'], r['session'])
        self.assertEqual(problem['event'], 'first')
        self.assertGreater(problem['oldest_pending_age_seconds'], 900)
        self.assertEqual(problem['status'], 'ATTENTION')

    def test_invalid_capacity_fails_loudly(self):
        r, p = self.assignment('invalid')
        with patch.dict(os.environ, FACTORY_CONTROLLER_TURNS='0'):
            with self.assertRaisesRegex(ValueError, 'positive'):
                c.run_turn(r['session'])
        self.assertEqual(c.read(p)['attempts'], 0)

    def test_concurrent_wrappers_and_inherited_slot_lock(self):
        assignments = [self.assignment(str(n), age=100 - n) for n in range(4)]
        # Real model subprocesses must inherit the slot fence. Every attempt to
        # acquire the occupied slot from that subprocess must fail.
        script = ('import sys,fcntl;sys.stdin.read();'
                  f'f=open({str(self.base / "slots/0.lock")!r},"a");'
                  '\ntry: fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)'
                  '\nexcept BlockingIOError: pass'
                  '\nelse: sys.exit(9)'
                  '\nprint(\'{"type":"turn.started"}\');'
                  'print(\'{"type":"turn.completed"}\')')
        with patch.dict(os.environ, FACTORY_CONTROLLER_TURNS='1'), self.fake(script):
            for _ in range(4):
                children = []
                for r, _ in reversed(assignments):
                    pid = os.fork()
                    if pid == 0:
                        try:
                            c.run_turn(r['session'])
                        except BaseException:
                            os._exit(1)
                        os._exit(0)
                    children.append(pid)
                for pid in children:
                    self.assertEqual(os.waitpid(pid, 0)[1], 0)
        for r, p in assignments:
            self.assertEqual(c.read(p)['status'], 'done')
            self.assertEqual(c.read(p)['attempts'], 1)

    def test_abandoned_claim_requires_recovery_and_winddown_allows_judgment(self):
        old, p = self.assignment('abandoned', age=100)
        e = c.read(p); e.update(status='running', attempts=1, run='crashed'); c.s.write(p, e)
        current, target = self.assignment('judgment', age=10)
        winddown = self.state / 'winddown/acme'
        winddown.parent.mkdir(parents=True); winddown.touch()
        with self.success():
            c.run_turn(old['session'])
            c.run_turn(current['session'])
        self.assertEqual(c.read(p)['status'], 'blocked')
        self.assertIn('abandoned', c.read(p)['attention'])
        self.assertEqual(c.read(target)['status'], 'done')


if __name__ == '__main__':
    unittest.main()
