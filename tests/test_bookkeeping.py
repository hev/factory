"""Real local Git copies; publication fixtures never contact GitHub."""
import hashlib
import json
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import test_dispatch as fixture
from test_dispatch import c, decision_context


class BookkeepingTest(unittest.TestCase):
    def setUp(self):
        self.f = fixture.DispatchTest()
        self.f.setUp()
        self.addCleanup(self.f.doCleanups)
        self.f.cfg.update(linear_team='team', plans_repo='acme/app', plans_branch='main')
        self.session = self.f.record['session']
        self.branch = 'bookkeeping/' + self.session
        self.f.git('-C', self.f.lane, 'branch', '-m', self.branch)
        self.f.git('-C', self.f.lane, 'update-ref', 'refs/remotes/origin/main', 'HEAD')
        self.content = b'> Approved source: https://example.invalid/issue/1\n\nApproved work.\n'
        Path(self.f.record['plan']).write_bytes(self.content)
        self.f.record.update(approval={'actor': 'human', 'team': 'team'},
                             source='https://example.invalid/issue/1',
                             approved_plan_sha256=hashlib.sha256(self.content).hexdigest())
        c.s.write(self.f.state / 'gaffers' / (self.session + '.json'), self.f.record)

    def prepare(self, publish=False):
        with decision_context(self.session):
            return c.bookkeeping.prepare(c.dispatch_context(), self.session, self.f.lane, publish)

    def test_exact_copy_one_commit_and_replay_without_extra_commit(self):
        first = self.prepare()
        self.assertEqual((self.f.lane / first['path']).read_bytes(), self.content)
        self.assertEqual(first['sha256'], hashlib.sha256(self.content).hexdigest())
        self.assertEqual(first['status'], 'prepared')
        self.assertEqual(self.prepare()['head'], first['head'])
        changed = c.s.run('git', '-C', self.f.lane, 'diff', '--name-only', first['base_head'], first['head']).stdout.strip()
        self.assertEqual(changed, first['path'])

    def test_source_change_and_missing_pin_are_rejected_before_git_mutation(self):
        Path(self.f.record['plan']).write_bytes(self.content + b'unapproved')
        with self.assertRaisesRegex(ValueError, 'bytes changed'):
            self.prepare()
        Path(self.f.record['plan']).write_bytes(self.content)
        del self.f.record['approved_plan_sha256']
        c.s.write(self.f.state / 'gaffers' / (self.session + '.json'), self.f.record)
        with self.assertRaisesRegex(ValueError, 'lack an intake digest'):
            self.prepare()
        self.assertFalse((self.f.lane / 'plans').exists())

    def test_pr_door_scope_hold_and_unacknowledged_turn_rejected(self):
        self.f.cfg.pop('linear_team')
        with self.assertRaisesRegex(ValueError, 'Linear'):
            self.prepare()
        self.f.cfg['linear_team'] = 'team'
        self.f.cfg['repo_scope'] = []
        with self.assertRaisesRegex(ValueError, 'scope'):
            self.prepare()
        self.f.cfg['repo_scope'] = ['acme/app']
        with patch.object(c.s, 'held', return_value=True):
            with self.assertRaisesRegex(ValueError, 'held'):
                self.prepare()
        with self.assertRaisesRegex(ValueError, 'running durable'):
            c.bookkeeping.prepare(c.dispatch_context(), self.session, self.f.lane)

    def test_dirty_lane_and_unrelated_committed_changes_are_preserved_and_rejected(self):
        unrelated = self.f.lane / 'unrelated'
        unrelated.write_text('keep me')
        with self.assertRaisesRegex(ValueError, 'clean'):
            self.prepare()
        self.assertEqual(unrelated.read_text(), 'keep me')
        unrelated.unlink()
        self.prepare()
        unrelated.write_text('also keep me')
        self.f.git('-C', self.f.lane, 'add', 'unrelated')
        with self.assertRaisesRegex(ValueError, 'unrelated'):
            self.prepare()
        self.assertEqual(unrelated.read_text(), 'also keep me')

    def test_interrupted_copy_resumes_but_different_bytes_do_not(self):
        real = c.s.run
        def crash(*args, **kwargs):
            if 'commit' in args:
                raise RuntimeError('fixture crash before commit')
            return real(*args, **kwargs)
        with patch.object(c.s, 'run', side_effect=crash):
            with self.assertRaisesRegex(RuntimeError, 'fixture crash'):
                self.prepare()
        target = self.f.lane / 'plans/active/plan.md'
        target.write_text('different')
        with self.assertRaisesRegex(ValueError, 'different bytes'):
            self.prepare()
        target.write_bytes(self.content)
        self.assertEqual(self.prepare()['status'], 'prepared')

    def test_publication_reuses_pr_after_lost_receipt_and_never_merges(self):
        real, calls, prs = c.s.run, [], []
        def publication(*args, **kwargs):
            if str(args[0]).endswith('factory-as.sh'):
                calls.append(args)
                if 'list' in args:
                    return SimpleNamespace(stdout=json.dumps(prs))
                if 'create' in args:
                    head = real('git', '-C', self.f.lane, 'rev-parse', 'HEAD').stdout.strip()
                    prs.append(dict(url='https://github.com/acme/app/pull/1', state='OPEN', headRefOid=head))
                    raise RuntimeError('fixture lost response after PR created')
                return SimpleNamespace(stdout='')
            return real(*args, **kwargs)
        with patch.object(c.s, 'run', side_effect=publication):
            with self.assertRaisesRegex(RuntimeError, 'lost response'):
                self.prepare(publish=True)
            receipt = self.prepare(publish=True)
        self.assertEqual(receipt['pr'], prs[0]['url'])
        self.assertEqual(sum('create' in args for args in calls), 1)
        self.assertTrue(all(args[1:3] == ('gaffer', '--') for args in calls))
        self.assertTrue(all('merge' not in args and '--force' not in args for args in calls))

    def test_symlinked_target_and_commissioned_lane_are_rejected(self):
        (self.f.lane / 'plans').symlink_to(self.f.root, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, 'clean'):
            self.prepare()
        (self.f.lane / 'plans').unlink()
        self.f.record['worktree_lanes'] = {str(self.f.lane): 'acme/app'}
        c.s.write(self.f.state / 'gaffers' / (self.session + '.json'), self.f.record)
        with self.assertRaisesRegex(ValueError, 'dedicated lane'):
            self.prepare()

    def test_git_clean_filter_cannot_silently_change_approved_bytes(self):
        repo = self.f.root / 'repo'
        info = repo / '.git/info/attributes'
        info.write_text('plans/active/*.md filter=fixture\n')
        self.f.git('-C', self.f.lane, 'config', 'filter.fixture.clean', "sed s/Approved/Changed/g")
        self.f.git('-C', self.f.lane, 'config', 'filter.fixture.smudge', 'cat')
        with self.assertRaisesRegex(ValueError, 'committed plan bytes'):
            self.prepare(publish=True)

    def test_closed_or_changed_pr_is_not_pushed_or_replaced(self):
        receipt, real = self.prepare(), c.s.run
        for state, head in [('CLOSED', receipt['head']), ('OPEN', 'different-head')]:
            calls = []
            def publication(*args, **kwargs):
                if str(args[0]).endswith('factory-as.sh'):
                    calls.append(args)
                    return SimpleNamespace(stdout=json.dumps([dict(state=state, headRefOid=head, url='fixture')]))
                return real(*args, **kwargs)
            with self.subTest(state=state, head=head), patch.object(c.s, 'run', side_effect=publication):
                with self.assertRaisesRegex(ValueError, 'PR requires owner recovery'):
                    self.prepare(publish=True)
            self.assertEqual(len(calls), 1)
            self.assertIn('list', calls[0])


if __name__ == '__main__':
    unittest.main()
