"""Real role/cache wrappers with a disposable home, repository and tmux stub."""
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class WorkerLaunchTest(unittest.TestCase):
    def test_new_terminal_preserves_argv_role_cache_and_legacy_shell(self):
        with tempfile.TemporaryDirectory(prefix='worker-launch-fixture-') as temp:
            root = Path(temp).resolve()
            install = root / 'install'; (install / 'scripts/lib').mkdir(parents=True)
            for name in ('factory-as.sh', 'factory-worker-cache.py'):
                shutil.copy2(ROOT / 'scripts' / name, install / 'scripts' / name)
            shutil.copy2(ROOT / 'scripts/lib/gh-auth.sh', install / 'scripts/lib/gh-auth.sh')
            (install / 'factories').mkdir()
            (install / 'factories/acme.toml').write_text('runtime="sessions"\n')
            (install / 'identity').mkdir()
            hook = install / 'identity/worker'
            hook.write_text('#!/bin/sh\nprintf fixture-worker-token'); hook.chmod(0o755)
            repo = root / 'repo'; repo.mkdir()
            subprocess.run(['git', 'init', str(repo)], check=True, capture_output=True)
            subprocess.run(['git', '-C', str(repo), 'remote', 'add', 'origin', 'https://github.com/acme/app.git'], check=True)
            binaries = root / 'bin'; binaries.mkdir()
            tmux = binaries / 'tmux'
            tmux.write_text('#!' + sys.executable + '\nimport json,sys;print(json.dumps(sys.argv[1:]))\n')
            tmux.chmod(0o755)
            env = dict(os.environ, HOME=str(root / 'home'), PATH=str(binaries) + ':' + os.environ['PATH'],
                       FACTORY_ROOT_DIR=str(install), FACTORY_ROLE='gaffer', FACTORY_INSTANCE='acme',
                       FACTORY_GAFFER_SESSION='gaffer-acme-plan', GH_TOKEN='fixture-parent-token')
            (root / 'home').mkdir()
            base = ['bash', str(install / 'scripts/factory-as.sh'), 'worker', '--', str(tmux), 'new-session',
                    '-d', '-s', 'worker-acme-task', '-c', str(repo)]
            payload = ['python3', '/path with spaces/boot.py', 'literal $() and `ticks`', 'two\nlines']
            result = subprocess.run(base + ['--'] + payload, env=env, text=True, capture_output=True, check=True)
            args = json.loads(result.stdout)
            self.assertIn('FACTORY_ROLE=worker', args)
            self.assertIn('FACTORY_GAFFER_SESSION=gaffer-acme-plan', args)
            self.assertIn('GH_TOKEN=fixture-worker-token', args)
            self.assertNotIn('GH_TOKEN=fixture-parent-token', args)
            command = shlex.split(args[-1])
            self.assertEqual(command[-len(payload):], payload)
            self.assertIn('hold', command)
            self.assertIn(str(root / 'home/.cache/cargo-target/acme--app'), command)
            shell = subprocess.run(base, env=env, text=True, capture_output=True, check=True)
            self.assertEqual(shlex.split(json.loads(shell.stdout)[-1])[-1], '-l')
            empty = subprocess.run(base + ['--'], env=env, text=True, capture_output=True)
            self.assertNotEqual(empty.returncode, 0)
            self.assertIn('empty', empty.stderr)


if __name__ == '__main__': unittest.main()
