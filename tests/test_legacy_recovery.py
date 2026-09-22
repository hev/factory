"""Attended repairs preserve history and never commission or retry work."""
import os
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from test_dispatch import c, d


class LegacyRecoveryTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory(prefix='legacy-recovery-')
        self.addCleanup(temp.cleanup)
        self.state = Path(temp.name)
        for obj, key, value in [(c, 'STATE', self.state), (c, 'BASE', self.state / 'controller'),
                                (c.s, 'STATE', self.state)]:
            self.enterContext(patch.object(obj, key, value))
        self.enterContext(patch.object(c.s, 'local_configs', return_value={'acme': {}}))
        self.enterContext(patch.dict(os.environ, {
            'FACTORY_ROLE': 'reception', 'FACTORY_EVENTS_DIR': str(self.state / 'events')
        }))
        self.session = 'gaffer-acme-task'
        self.record = dict(session=self.session, instance='acme', transport='exec', status='running',
                           tasks=[dict(id='old', status='source_review_passed', worker='legacy-worker')],
                           worktree_lanes=[dict(path='/workspace/lane', repo='acme/app', owner=self.session)],
                           approval={'actor': 'human'}, source_paused=True)
        self.path = self.state / 'gaffers' / (self.session + '.json')
        c.s.write(self.path, self.record)
        self.spool = self.state / 'events/acme.jsonl'
        self.spool.parent.mkdir()
        self.spool.write_bytes(b'{"from":"unowned"}\ntest\n{"from":"unowned"}\n')

    def observe(self):
        d.observe(c, dict(session=self.session, instance='acme', tasks=[]))

    def test_exact_quarantine_preserves_spool_and_later_records(self):
        before = self.spool.read_bytes()
        with self.assertRaisesRegex(ValueError, 'line 2'):
            self.observe()
        c.quarantine_spool('acme', '2', 'inspected historical test line')
        self.assertEqual(self.spool.read_bytes(), before)
        self.observe()
        with self.spool.open('ab') as f:
            f.write(b'new corruption\n')
        with self.assertRaisesRegex(ValueError, 'line 4'):
            self.observe()
        receipt = c.read(c.BASE / 'recovery/spool/acme/2.json')
        self.assertEqual(bytes.fromhex(receipt['raw_hex']), b'test\n')

    def test_quarantine_does_not_survive_changed_prefix_or_changed_line(self):
        c.quarantine_spool('acme', '2', 'inspected')
        self.spool.write_bytes(b'{"from":"different"}\ntest\n')
        with self.assertRaisesRegex(ValueError, 'line 2'):
            self.observe()
        with self.assertRaisesRegex(ValueError, 'existing quarantine differs'):
            c.quarantine_spool('acme', '2', 'replace receipt')
        self.spool.write_bytes(b'{"from":"unowned"}\nevil\n')
        with self.assertRaisesRegex(ValueError, 'line 2'):
            self.observe()

    def test_valid_event_and_partial_tail_cannot_be_quarantined(self):
        with self.assertRaisesRegex(ValueError, 'valid event'):
            c.quarantine_spool('acme', '1', 'not allowed')
        self.spool.write_bytes(b'{"from":"unowned"}\npartial')
        self.observe()
        with self.assertRaisesRegex(ValueError, 'complete existing line'):
            c.quarantine_spool('acme', '2', 'not allowed')

    def test_non_object_record_requires_explicit_disposition(self):
        self.spool.write_bytes(b'null\n')
        with self.assertRaisesRegex(ValueError, 'line 1'):
            self.observe()
        c.quarantine_spool('acme', '1', 'inspected null')
        self.observe()

    def test_owned_completion_after_quarantine_is_still_observed(self):
        with self.spool.open('ab') as f:
            f.write((json.dumps({'instance': 'acme', 'from': 'worker-owned', 'kind': 'done'}) + '\n').encode())
        record = dict(session=self.session, instance='acme', tasks=[
            dict(id='implement', session='worker-owned', status='running', repo='acme/app',
                 worktree='/fixture/lane', brief='/fixture/brief', kind='implementation', after=[]),
            dict(id='review', session='worker-next', status='pending', repo='acme/app',
                 worktree='/fixture/lane', brief='/fixture/brief', kind='review', after=['implement'])])
        c.quarantine_spool('acme', '2', 'inspected')
        d.observe(c, record)
        self.assertEqual(record['tasks'][0]['status'], 'done')
        self.assertEqual(record['tasks'][1]['status'], 'pending')
        self.assertFalse((c.BASE / 'queues').exists())

    def test_archive_preserves_full_record_lanes_and_approval_without_dispatch(self):
        c.archive_legacy_tasks(self.session, 'legacy checklist verified')
        result = c.read(self.path)
        self.assertEqual(c.read(Path(result['legacy_execution']['archive'])), self.record)
        self.assertEqual(result['legacy_execution']['tasks'], self.record['tasks'])
        self.assertEqual(result['worktree_lanes'], {'/workspace/lane': 'acme/app'})
        self.assertEqual(result['approval'], self.record['approval'])
        self.assertTrue(result['source_paused'])
        self.assertEqual(result['tasks'], [])
        self.assertNotIn('owner', result)
        self.assertNotIn('commissions', result)
        self.assertFalse((c.BASE / 'queues').exists())
        c.archive_legacy_tasks(self.session, 'idempotent repeat')
        self.assertEqual(c.read(self.path), result)

    def test_commissioned_or_executable_tasks_cannot_be_archived(self):
        for extra in [{'owner': self.session}, {'commissions': [{'run': 'real'}]},
                      {'tasks': [{'id': 'real', 'session': 'worker-real'}]}]:
            record = dict(self.record, **extra)
            c.s.write(self.path, record)
            with self.assertRaisesRegex(ValueError, 'only uncommissioned'):
                c.archive_legacy_tasks(self.session, 'not permitted')
            self.assertEqual(c.read(self.path), record)

    def test_assignment_and_poll_locks_fence_repair(self):
        for lock in [c.BASE / 'poll.lock', c.BASE / 'locks' / (self.session + '.lock')]:
            with c.gate(lock):
                with self.assertRaisesRegex(ValueError, 'active'):
                    c.archive_legacy_tasks(self.session, 'cannot race')
            self.assertEqual(c.read(self.path), self.record)

    def test_foreign_lanes_and_unattended_repairs_are_rejected(self):
        record = dict(self.record, worktree_lanes=[dict(path='/lane', repo='acme/app', owner='foreign')])
        c.s.write(self.path, record)
        with self.assertRaisesRegex(ValueError, 'lane ownership'):
            c.archive_legacy_tasks(self.session, 'not ours')
        for role in ['foreman', 'gaffer', 'worker']:
            with patch.dict(os.environ, FACTORY_ROLE=role):
                with self.assertRaisesRegex(ValueError, 'attended operator'):
                    c.archive_legacy_tasks(self.session, 'not authorized')
                with self.assertRaisesRegex(ValueError, 'attended operator'):
                    c.quarantine_spool('acme', '2', 'not authorized')
        with self.assertRaisesRegex(ValueError, 'reason'):
            c.quarantine_spool('acme', '2', '')
