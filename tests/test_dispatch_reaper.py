"""Real reaper branch with isolated terminal/cleanup commands and state."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class DispatchReaperTest(unittest.TestCase):
    def test_completed_review_harvest_preserves_protections_and_owner(self):
        with tempfile.TemporaryDirectory(prefix='dispatch-reaper-fixture-') as temp:
            root = Path(temp); scripts = root / 'install/scripts'; scripts.mkdir(parents=True)
            reaper = scripts / 'factory-reap.sh'
            reaper.write_text((ROOT / 'scripts/factory-reap.sh').read_text().replace(
                'export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"', '# fixture PATH is isolated'))
            configs = root / 'install/factories'; configs.mkdir()
            (configs / 'acme.toml').write_text('runtime="sessions"\nrepo_scope=["acme/app"]\nidle_minutes="1"\n')
            cleaner = scripts / 'factory-clean-worktrees.py'
            cleaner.write_text('import os,sys\nfrom pathlib import Path\nif sys.argv[1]=="sweep": Path(os.environ["SWEEP_MARKER"]).touch()\nsys.exit(1 if os.environ["CASE"]=="cleanup-failure" else 0)\n')
            bins = root / 'bin'; bins.mkdir()
            (bins / 'python3').symlink_to(sys.executable)
            for name, body in {
                'tmux': '''import os,sys,json,time
from pathlib import Path
args=sys.argv[1:];path=Path(os.environ['FLOOR']);live=json.loads(path.read_text());case=os.environ['CASE']
if args[0]=='list-sessions':
 for name in live:
  print(name+'|'+str(int(time.time()) if case=='active' else 0)+'|'+('1' if case=='attached' else '0'))
elif args[0]=='has-session': sys.exit(0 if args[-1].lstrip('=') in live else 1)
elif args[0]=='display-message': print('/fixture/worktree' if args[-1]=='#{pane_current_path}' else '123')
elif args[0]=='capture-pane': print('independent review completed; evidence retained')
elif args[0]=='kill-session':
 live.remove(args[-1]);path.write_text(json.dumps(live))
else: raise RuntimeError('unexpected terminal operation')
''',
                'pgrep': 'print("456")\n',
                'ps': 'print("codex")\n',
            }.items():
                path = bins / name; path.write_text('#!' + sys.executable + '\n' + body); path.chmod(0o755)
            state = root / 'state'; children = state / 'children'; children.mkdir(parents=True)
            home = root / 'home'; home.mkdir()
            floor = root / 'floor.json'
            env = dict(os.environ, HOME=str(home), PATH=str(bins) + ':' + os.environ['PATH'],
                       FACTORY_STATE_DIR=str(state), FACTORY_LEDGER_DIR=str(children),
                       FACTORY_GAFFER_SESSION='gaffer-acme-plan', FLOOR=str(floor), SWEEP_MARKER=str(root/'swept'))
            owned = children / 'worker-acme-review.json'
            foreign = children / 'worker-acme-foreign.json'
            foreign.write_text(json.dumps(dict(session=foreign.stem, instance='acme', parent='gaffer-acme-other')))
            for case in ('attached', 'active', 'ci', 'cleanup-failure', 'uncompleted', 'deferred', 'completed'):
                floor.write_text(json.dumps([owned.stem, foreign.stem]))
                child = dict(session=owned.stem, instance='acme', parent='gaffer-acme-plan', repo='acme/app')
                if case != 'uncompleted': child['completed_at'] = 'fixture-completion'
                owned.write_text(json.dumps(child))
                watch = state / 'ci/acme/watch.json'
                watch.parent.mkdir(parents=True, exist_ok=True)
                if case == 'ci': watch.write_text(json.dumps(dict(instance='acme', worker=owned.stem)))
                elif watch.exists(): watch.unlink()
                marker=root/'swept'
                if marker.exists():marker.unlink()
                result = subprocess.run(['bash', str(reaper), 'acme'], env=dict(env, CASE=case, FACTORY_DEFER_WORKTREE_CLEANUP='1' if case=='deferred' else '0'),
                                        text=True, capture_output=True, timeout=15)
                self.assertEqual(result.returncode, 1 if case == 'cleanup-failure' else 0, result.stderr)
                remaining = json.loads(floor.read_text())
                self.assertIn(foreign.stem, remaining)
                self.assertTrue(foreign.exists())
                self.assertEqual(marker.exists(),case!='deferred')
                if case in ('completed','deferred'):
                    self.assertNotIn(owned.stem, remaining)
                    self.assertFalse(owned.exists())
                    log = state / 'harvest/acme/worker-acme-review.log'
                    self.assertIn('independent review completed', log.read_text())
                    self.assertIn('owned task completed', result.stdout)
                else:
                    self.assertIn(owned.stem, remaining, case)
                    self.assertTrue(owned.exists(), case)


if __name__ == '__main__': unittest.main()
