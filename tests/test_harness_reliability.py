import concurrent.futures
from datetime import datetime, timezone
import json
import multiprocessing
import os
from pathlib import Path
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import factory_harness as h

FIXTURES = Path(__file__).parent / 'fixtures' / 'harness'
REFUSAL = (FIXTURES / 'codex-refusal.jsonl').read_text()
IDENTITY = dict(owner='gaffer-example-task', task='implement', event='approved-1')


def observe_process(root, ident):
    h.Store(root, 'host-a').observe('codex', str(ident), h.Classification('usage_limit', None, True), 1)


def interrupted(root):
    def crash(*args):
        os._exit(23)
    h.run_attempt(h.Store(root, 'host-a'), 'crash', IDENTITY, 'brief', 'codex', {}, crash, 0)


class HarnessTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.store = h.Store(self.temp.name, 'host-a')

    def attempt(self, launch, ident='a', config=None):
        return h.run_attempt(self.store, ident, IDENTITY, 'same brief', 'codex', config or {}, launch, 0)

    def test_recorded_refusal_and_timezone(self):
        result = h.classify('codex', REFUSAL, 1)
        self.assertEqual(result.kind, 'usage_limit')
        self.assertEqual(result.reset, '2026-09-19T02:12')
        self.assertTrue(result.safe_to_fallback)
        self.assertEqual(result.credits['balance'], '0')
        self.assertIsNone(h.reset_epoch(result.reset))
        self.assertEqual(h.reset_epoch(result.reset, timezone.utc),
                         datetime(2026, 9, 19, 2, 12, tzinfo=timezone.utc).timestamp())
        self.assertEqual(h.reset_epoch('2026-09-19T02:12:33+02:00'),
                         datetime(2026, 9, 19, 0, 12, 33, tzinfo=timezone.utc).timestamp())

    def test_credit_alone_never_refusal(self):
        self.assertEqual(h.classify('codex', REFUSAL.splitlines()[0], 1).kind, 'other')

    def test_exec_and_claude_recorded_shapes(self):
        capacity = (FIXTURES / 'codex-exec-capacity.jsonl').read_text()
        self.assertEqual(h.classify('codex', capacity, 1).kind, 'other')
        success = '{"type":"turn.started"}\n{"type":"turn.completed","usage":{}}'
        self.assertEqual(h.classify('codex', success, 0).kind, 'ok')
        self.assertEqual(h.classify('codex', success, 1).kind, 'other')
        self.assertEqual(h.classify('codex', '{"type":"turn.completed"}', 0).kind, 'other')
        self.assertEqual(h.classify('claude', (FIXTURES / 'claude-auth.jsonl').read_text(), 1).kind, 'auth')
        self.assertEqual(h.classify('claude', '{"type":"result"}', 0).kind, 'other')

    def test_activity_and_malformed_output_prevent_replay(self):
        for row in [dict(type='item.started'), dict(type='response_item', payload=dict(role='assistant')),
                    dict(type='response_item', payload=dict(type='function_call'))]:
            result = h.classify('codex', json.dumps(row) + '\n' + REFUSAL, 1)
            self.assertFalse(result.safe_to_fallback)
        for prefix in ('garbage', '[]', '{"type":"event_msg","payload":null}'):
            self.assertEqual(h.classify('codex', prefix + '\n' + REFUSAL, 1).kind, 'other')

    def test_one_beat_suppression_and_reset(self):
        reset = '1970-01-01T00:01:00+00:00'
        self.store.observe('codex', 'r', h.Classification('usage_limit', reset, True), 0)
        self.assertTrue(all(not self.store.admitted('codex', t) for t in range(60)))
        self.assertTrue(self.store.admitted('codex', 60))
        self.assertTrue(self.store.admitted('claude', 0))
        self.assertTrue(h.Store(self.temp.name, 'host-b').admitted('codex', 0))
        self.store.observe('codex', 'r', h.Classification('usage_limit', reset, True), 61)
        self.assertTrue(self.store.admitted('codex', 61))

    def test_unknown_reset_manual_state_and_reports(self):
        manual = Path(self.temp.name) / 'example'
        manual.write_text('manual hold')
        winddown = Path(self.temp.name) / 'winddown'
        winddown.write_text('stop')
        self.store.observe('codex', 'r', h.Classification('usage_limit', None, True), 0)
        self.assertFalse(self.store.admitted('codex', 10**10))
        reports = self.store.pending_reports()
        self.assertEqual(reports, h.Store(self.temp.name, 'host-a').pending_reports())
        for i in range(10):
            self.store.observe('codex', str(i), h.Classification('usage_limit', None, True), i)
        self.assertEqual(len(self.store.pending_reports()), 1)
        self.store.acknowledge_report(reports[0]['id'])
        self.assertEqual(self.store.pending_reports(), [])
        self.assertEqual(manual.read_text(), 'manual hold')
        self.assertEqual(winddown.read_text(), 'stop')

    def test_concurrent_observation_and_corrupt_state(self):
        with concurrent.futures.ProcessPoolExecutor(max_workers=4) as pool:
            list(pool.map(observe_process, [self.temp.name] * 16, range(16)))
        self.assertEqual(len(self.store.pending_reports()), 1)
        self.assertEqual(len(json.loads(self.store.path.read_text())['observations']), 16)
        self.store.path.write_text('{broken')
        with self.assertRaises(ValueError):
            self.store.admitted('codex', 0)

    def test_atomic_replace_failure_preserves_old_state(self):
        self.store.observe('codex', 'a', h.Classification('usage_limit', '1970-01-01T00:01+00:00'), 0)
        original = self.store.path.read_bytes()
        with patch.object(h.os, 'replace', side_effect=OSError('crash before replace')):
            with self.assertRaises(OSError):
                self.store.observe('claude', 'b', h.Classification('usage_limit'), 0)
        self.assertEqual(self.store.path.read_bytes(), original)

    def test_identical_brief_identity_and_separate_models_on_fallback(self):
        calls = []
        def launch(*args):
            calls.append(args)
            return h.classify('codex', REFUSAL, 1) if args[0] == 'codex' else h.Classification('ok')
        cfg = {'harness_models': {'codex': 'primary-model', 'claude': 'fallback-model'}}
        receipt = self.attempt(launch, config=cfg)
        self.assertEqual(receipt['status'], 'ok')
        self.assertEqual([c[0] for c in calls], ['codex', 'claude'])
        self.assertEqual([c[1] for c in calls], ['primary-model', 'fallback-model'])
        self.assertEqual(calls[0][2:], calls[1][2:])
        self.assertEqual(self.attempt(launch, config=cfg), receipt)
        self.assertEqual(len(calls), 2)

    def test_both_refuse_no_recursion(self):
        calls = []
        def launch(*args):
            calls.append(args[0])
            return h.Classification('usage_limit', safe_to_fallback=True)
        result = self.attempt(launch)
        self.assertEqual(result['status'], 'blocked')
        self.assertEqual(calls, ['codex', 'claude'])
        self.attempt(launch)
        self.assertEqual(len(calls), 2)
        self.assertFalse(self.store.admitted('claude', 100))

    def test_auth_other_and_post_work_limit_never_replay(self):
        for n, result in enumerate([h.Classification('auth'), h.Classification('other'),
                                    h.Classification('usage_limit')]):
            calls = []
            def launch(*args):
                calls.append(args)
                return result
            self.assertEqual(self.attempt(launch, str(n))['status'], 'blocked')
            self.assertEqual(len(calls), 1)

    def test_held_primary_default_fallback_and_disabled_fallback(self):
        self.store.observe('codex', 'r', h.Classification('usage_limit'), 0)
        calls = []
        def launch(*args):
            calls.append(args)
            return h.Classification('ok')
        self.assertEqual(self.attempt(launch)['status'], 'ok')
        self.assertEqual(calls[0][:2], ('claude', None))
        self.assertEqual(self.attempt(launch, 'disabled', {'harness_fallback': ''})['status'], 'blocked')
        self.assertEqual(len(calls), 1)

    def test_crashed_reserved_attempt_never_relaunches(self):
        proc = multiprocessing.get_context('spawn').Process(target=interrupted, args=(self.temp.name,))
        proc.start(); proc.join(10)
        self.assertEqual(proc.exitcode, 23)
        def forbidden(*args):
            self.fail('crashed attempt relaunched')
        result = h.run_attempt(self.store, 'crash', IDENTITY, 'brief', 'codex', {}, forbidden, 100)
        self.assertEqual(result['status'], 'blocked')
        self.assertEqual(result['harnesses'][0]['status'], 'reserved')

    def test_concurrent_same_attempt_launches_once(self):
        entered = threading.Event()
        release = threading.Event()
        calls = []
        def launch(*args):
            calls.append(args)
            entered.set()
            self.assertTrue(release.wait(5))
            return h.Classification('ok')
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            first = pool.submit(self.attempt, launch)
            self.assertTrue(entered.wait(5))
            duplicate = pool.submit(self.attempt, launch).result(5)
            self.assertEqual(duplicate['status'], 'blocked')
            release.set()
            self.assertEqual(first.result(5)['status'], 'ok')
        self.assertEqual(len(calls), 1)

    def test_unknown_activity_never_authorizes_replay(self):
        stream = '{"type":"event_msg","payload":{"type":"future_tool_event"}}\n' + REFUSAL
        self.assertFalse(h.classify('codex', stream, 1).safe_to_fallback)

    def test_same_harness_and_invalid_config(self):
        calls = []
        def launch(*args):
            calls.append(args)
            return h.Classification('usage_limit', safe_to_fallback=True)
        self.assertEqual(self.attempt(launch, config={'harness_fallback': 'codex'})['status'], 'blocked')
        self.assertEqual(len(calls), 1)
        for config in ({'harness_fallback': 'unknown'}, {'harness_models': {'other': 'model'}}):
            with self.assertRaises(ValueError):
                self.attempt(launch, 'bad', config)

    def test_changed_identity_rejected(self):
        self.attempt(lambda *a: h.Classification('ok'))
        with self.assertRaises(ValueError):
            h.run_attempt(self.store, 'a', IDENTITY, 'changed', 'codex', {}, lambda *a: None, 0)


if __name__ == '__main__':
    unittest.main()
