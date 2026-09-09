import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('sessions', Path(__file__).resolve().parents[1] / 'scripts/factory-session.py')
sessions = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sessions)
launch_session = sessions.launch


class SessionsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.ws = self.root / 'workspace'
        self.plan = self.ws / 'plans/active/example.md'
        self.plan.parent.mkdir(parents=True)
        self.plan.write_text('approved work')
        self.cfg = {'runtime': 'sessions', 'home_host': 'fixture', 'workspace_path': str(self.ws)}
        for p in (patch.object(sessions, 'STATE', self.root / 'state'),
                  patch.object(sessions, 'configs', return_value={'acme': self.cfg}),
                  patch.dict(os.environ, FACTORY_ROLE='foreman', FACTORY_HOSTNAME_OVERRIDE='fixture')):
            p.start(); self.addCleanup(p.stop)
        self.launch = patch.object(sessions, 'launch').start()
        self.addCleanup(patch.stopall)

    def test_codex_mcp_override_has_the_exact_configured_server_name(self):
        import tomllib
        cfg = dict(self.cfg, linear_team='ENG', linear_mcp_server='linear-bot')
        with patch.object(sessions, 'local_configs', return_value={'acme': cfg}), patch.object(sessions, 'run') as invoke:
            launch_session('foreman', 'foreman', {'harness':'codex','model':'fixture'}, self.ws, 'brief')
            argv = invoke.call_args.args
            overrides = [argv[i+1] for i,v in enumerate(argv[:-1]) if v == '-c']
            mcp = next(v for v in overrides if isinstance(v, str) and v.startswith('mcp_servers='))
            parsed = tomllib.loads(mcp)['mcp_servers']
            self.assertEqual(list(parsed), ['linear-bot'])
            self.assertEqual(parsed['linear-bot']['command'], 'python3')

    def test_start_is_idempotent_and_plan_has_one_owner(self):
        with patch.object(sessions, 'alive', return_value=False):
            self.assertEqual(sessions.start_gaffer('acme', 'example', self.plan), 'gaffer-acme-example')
        self.assertEqual(self.launch.call_count, 1)
        with patch.object(sessions, 'alive', return_value=True):
            sessions.start_gaffer('acme', 'example', self.plan)
            with self.assertRaisesRegex(ValueError, 'already has'):
                sessions.start_gaffer('acme', 'duplicate', self.plan)
        self.assertEqual(self.launch.call_count, 1)

    def test_host_hold_and_role_boundaries_prevent_launch(self):
        for overrides in ({'FACTORY_ROLE':'worker'}, {'FACTORY_HOSTNAME_OVERRIDE':'laptop'}):
            with patch.dict(os.environ, overrides), self.assertRaises(ValueError):
                sessions.start_gaffer('acme', 'example', self.plan)
        hold = sessions.STATE / 'holds/acme'
        hold.parent.mkdir(parents=True); hold.touch()
        with self.assertRaisesRegex(ValueError, 'held'):
            sessions.start_gaffer('acme', 'example', self.plan)
        self.launch.assert_not_called()

    def test_wrong_plan_and_session_reuse_rejected(self):
        with self.assertRaisesRegex(ValueError, 'plans/active'):
            sessions.start_gaffer('acme', 'bad', self.root / 'elsewhere.md')
        with patch.object(sessions, 'alive', return_value=False):
            sessions.start_gaffer('acme', 'example', self.plan)
            second = self.plan.with_name('second.md'); second.write_text('other plan')
            with self.assertRaisesRegex(ValueError, 'another plan'):
                sessions.start_gaffer('acme', 'example', second)

    def test_retire_refuses_live_worker_ownership(self):
        with patch.object(sessions, 'alive', return_value=False):
            session = sessions.start_gaffer('acme', 'example', self.plan)
        sessions.write(sessions.STATE / 'children/worker.json', {'parent': session})
        with self.assertRaisesRegex(ValueError, 'still owns workers'):
            sessions.retire(session)

    def test_health_requires_reconciliation_not_just_tmux(self):
        with patch.object(sessions, 'alive', return_value=True):
            self.assertEqual(sessions.health('acme'), 1)
            sessions.write(sessions.STATE / 'foreman/ready.json', {'ts': sessions.stamp(), 'instances': ['acme']})
            self.assertEqual(sessions.health('acme'), 0)

    def test_literal_message_persists_without_starting_a_session(self):
        body = 'quotes " and $(touch /bad)\nsecond line'
        with patch.object(sessions, 'alive', return_value=False):
            sessions.message('acme', 'steer', body)
        saved = list((sessions.STATE / 'foreman/inbox').glob('*.json'))
        self.assertEqual(json.loads(saved[0].read_text())['msg'], body)
        self.launch.assert_not_called()


if __name__ == '__main__':
    unittest.main()
