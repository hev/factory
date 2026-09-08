"""Isolated health/reaper checks; never touch the host's sessions or state.

Run: python3 -m unittest discover -s scripts/tests -p 'test_preview_browser.py'
"""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class PreviewBrowserTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        for directory in ('scripts', 'factories', 'bin', 'state/.factory/heartbeat'):
            (self.root / directory).mkdir(parents=True)
        self.env = dict(os.environ, PATH=str(self.root / 'bin'),
                        FACTORY_TEST_HOME=str(self.root / 'state'),
                        FACTORY_HEALTH_LOCAL='1', FACTORY_DISK_FREE_PERCENT='0',
                        CALLS=str(self.root / 'calls'))
        # Redirect only fixture copies, so neither real HOME nor host PATH is changed.
        for name in ('factory-health.sh', 'factory-reap.sh'):
            text = (ROOT / 'scripts' / name).read_text()
            text = text.replace('export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"', '')
            text = text.replace('$HOME', '$FACTORY_TEST_HOME')
            (self.root / 'scripts' / name).write_text(text)
        for name in ('dirname', 'awk', 'date', 'df', 'jq', 'mkdir', 'cat', 'sed', 'rm', 'basename'):
            os.symlink(shutil.which(name), self.root / 'bin' / name)
        shutil.copy(ROOT / 'scripts/factory-clean-worktrees.py', self.root / 'scripts')
        os.symlink(sys.executable, self.root / 'bin/python3')
        self.stub('gh', 'exit 0')
        self.config()
        (self.root / 'state/.factory/heartbeat/demo').touch()

    def config(self, value='["*.example.test", "example.test"]'):
        text = 'runtime = "one-shot"\n'
        if value is not None:
            text += 'preview_domains = ' + value + '\n'
        (self.root / 'factories/demo.toml').write_text(text)

    def stub(self, name, body):
        path = self.root / 'bin' / name
        path.write_text('#!/bin/bash\n' + body + '\n')
        path.chmod(0o755)

    def run_script(self, name, *args):
        return subprocess.run(['/bin/bash', str(self.root / 'scripts' / name),
                               'demo', *args], env=self.env, text=True,
                              capture_output=True, timeout=10)

    def doctor(self, report, status=0):
        self.stub('agent-browser', 'printf "%s\\n" "$*" >> "$CALLS"\n'
                  + "printf '%s\\n' '" + report + "'\nexit " + str(status))

    def test_health_opt_in_and_missing_binary(self):
        self.config(None)
        self.assertEqual(self.run_script('factory-health.sh').returncode, 0)
        for config in ('[]', '[\n "*.example.test",\n]'):
            self.config(config)
            result = self.run_script('factory-health.sh')
            self.assertEqual(result.returncode, 1)
            self.assertIn('MISSING required command: agent-browser', result.stdout)

    def test_health_doctor_reports(self):
        for report, status, expected in (
            ('{"success":true,"summary":{"fail":0,"warn":0}}', 0, 0),
            ('{"success":true,"summary":{"fail":0,"warn":1}}', 0, 0),
            ('{"success":false,"summary":{"fail":1}}', 0, 1),
            ('{"success":true,"summary":{"fail":1}}', 0, 1),
            ('not json', 0, 1), ('{}', 0, 1),
            ('{"success":true,"summary":{"fail":0}}', 1, 1),
        ):
            with self.subTest(report=report, status=status):
                self.doctor(report, status)
                self.assertEqual(self.run_script('factory-health.sh').returncode, expected)
        self.assertEqual(set((self.root / 'calls').read_text().splitlines()),
                         {'doctor --offline --quick --json'})

    def setup_workers(self):
        ledger = self.root / 'state/.factory/children'
        ledger.mkdir(parents=True)
        for session in ('worker-demo-one', 'worker-demo-gone', 'worker-other-one'):
            instance = 'other' if 'other' in session else 'demo'
            (ledger / (session + '.json')).write_text(
                '{"instance":"' + instance + '","pr":12}')
        evidence = self.root / 'state/.factory/evidence/demo/worker-demo-one'
        evidence.mkdir(parents=True)
        (evidence / 'criterion.png').write_bytes(b'fixture')
        (evidence / 'browser.log').write_text('allowlist refused example.invalid; exit 1\n')
        self.stub('tmux', '''case "$1" in
list-sessions) printf 'worker-demo-one|1|0\\nworker-other-one|1|0\\nworker-demo-live|1|1\\n' ;;
display-message) echo 0 ;;
capture-pane) echo 'worker pane' ;;
has-session) [[ "$3" != '=worker-demo-gone' ]] ;;
kill-session) printf '%s\\n' "$*" >> "$CALLS" ;;
esac''')
        self.stub('agent-browser', 'printf "%s\\n" "$*" >> "$CALLS"\nexit "${CLOSE_STATUS:-0}"')
        return ledger, evidence

    def test_reap_isolated_sessions_orphan_and_retention(self):
        ledger, evidence = self.setup_workers()
        result = self.run_script('factory-reap.sh')
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = (self.root / 'calls').read_text().splitlines()
        self.assertEqual(calls, ['--session worker-demo-one close',
                                 'kill-session -t worker-demo-one',
                                 '--session worker-demo-gone close'])
        self.assertTrue((ledger / 'worker-other-one.json').exists())
        self.assertFalse((ledger / 'worker-demo-one.json').exists())
        self.assertTrue((evidence / 'criterion.png').exists())
        log = self.root / 'state/.factory/harvest/demo/worker-demo-one.log'
        self.assertIn('allowlist refused example.invalid; exit 1', log.read_text())

    def test_reap_absent_config_without_evidence_skips_browser(self):
        _, evidence = self.setup_workers()
        self.config(None)
        shutil.rmtree(evidence)
        result = self.run_script('factory-reap.sh')
        self.assertEqual(result.returncode, 0)
        self.assertEqual((self.root / 'calls').read_text().splitlines(),
                         ['kill-session -t worker-demo-one'])

    def test_reap_dry_run_and_failed_close(self):
        ledger, _ = self.setup_workers()
        self.assertEqual(self.run_script('factory-reap.sh', '--dry-run').returncode, 0)
        self.assertFalse((self.root / 'calls').exists())
        self.assertTrue((ledger / 'worker-demo-gone.json').exists())
        self.env['CLOSE_STATUS'] = '1'
        result = self.run_script('factory-reap.sh')
        self.assertEqual(result.returncode, 0)
        self.assertIn('close failed', result.stderr)
        log = self.root / 'state/.factory/harvest/demo/worker-demo-one.log'
        self.assertIn('# agent-browser close failed', log.read_text())


if __name__ == '__main__':
    unittest.main()
