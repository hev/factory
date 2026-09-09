#!/usr/bin/env python3
"""Disposable in-scope git, launcher and reaper fixtures; no real API calls."""
import fcntl
import signal
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]

def run(*args, env=None, cwd=None):
    result = subprocess.run(args, env=env, cwd=cwd, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    assert result.returncode == 0, (args, result.stdout)
    return result.stdout.strip()

with tempfile.TemporaryDirectory(prefix='factory-cache-fixture-') as tmp:
    home = Path(tmp).resolve()
    repo = home / 'repo'
    run('git', 'init', str(repo))
    run('git', '-C', str(repo), 'config', 'user.email', 'fixture@example.invalid')
    run('git', '-C', str(repo), 'config', 'user.name', 'Fixture')
    (repo / '.gitignore').write_text('target/\nother-cache/\n')
    (repo / 'Cargo.toml').write_text('[package]\nname="factory-cache-fixture"\nversion="0.1.0"\nedition="2021"\n[dependencies]\nfixture-dependency={path="dep"}\n')
    (repo / 'src').mkdir()
    (repo / 'src/main.rs').write_text('fn main() { println!("{}", fixture_dependency::value()); }\n')
    (repo / 'dep/src').mkdir(parents=True)
    (repo / 'dep/Cargo.toml').write_text('[package]\nname="fixture-dependency"\nversion="0.1.0"\nedition="2021"\n')
    (repo / 'dep/src/lib.rs').write_text('pub fn value() -> u32 { 42 }\n')
    run('git', '-C', str(repo), 'add', '.')
    run('git', '-C', str(repo), 'commit', '-m', 'fixture')
    run('git', '-C', str(repo), 'remote', 'add', 'origin', 'https://github.com/acme/factory.git')
    first, second = home / 'first', home / 'second'
    for p in (first, second):
        run('git', '-C', str(repo), 'worktree', 'add', '-b', p.name, str(p))
    env = dict(os.environ, HOME=str(home), CARGO_HOME=os.environ.get('CARGO_HOME', str(Path.home()/'.cargo')), RUSTUP_HOME=os.environ.get('RUSTUP_HOME', str(Path.home()/'.rustup')))
    launcher = ROOT / 'scripts/factory-as.sh'
    run('bash', str(launcher), 'worker', '--', 'cargo', 'build', '--offline', '-v', cwd=first, env=env)
    started = time.monotonic()
    warm = run('bash', str(launcher), 'worker', '--', 'cargo', 'build', '--offline', '-v', cwd=second, env=env)
    elapsed = time.monotonic() - started
    assert elapsed < 120 and 'Fresh fixture-dependency' in warm, warm
    assert not (first / 'target').exists() and not (second / 'target').exists()
    print(f'PASS: two worker worktrees reuse compiled dependency; second build {elapsed:.3f}s')
    # Real launcher lease prevents an exclusive maintainer while a worker is
    # alive, and a terminal interrupt must not tear down its lease supervisor.
    ready, release = home/'ready', home/'release'
    sleeper = home/'sleeper.py'
    sleeper.write_text("import pathlib,time\nr=pathlib.Path(" + repr(str(ready)) + "); r.touch()\np=pathlib.Path(" + repr(str(release)) + ")\nwhile not p.exists(): time.sleep(0.02)\n")
    worker = subprocess.Popen(['python3', str(ROOT/'scripts/factory-worker-cache.py'), 'hold', str(home/'.cache/cargo-target/acme--factory'), 'python3', str(sleeper)], env=env)
    try:
        deadline = time.monotonic() + 10
        while not ready.exists() and time.monotonic() < deadline:
            time.sleep(0.02)
        assert ready.exists()
        with (home/'.cache/cargo-target/.locks/acme--factory').open('a') as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                raise AssertionError('worker did not hold lease')
            except BlockingIOError:
                pass
            worker.send_signal(signal.SIGINT)
            time.sleep(0.05)
            assert worker.poll() is None
            release.touch()
            assert worker.wait(timeout=10) == 0
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        print('PASS: real worker holds shared lease, survives supervisor SIGINT, releases on completion')
    finally:
        release.touch()
        worker.wait(timeout=10)
    bin_dir = home / 'bin'
    bin_dir.mkdir()
    stub = '''#!/usr/bin/env python3
import os,sys,json
from pathlib import Path
name=Path(sys.argv[0]).name
case=os.environ.get('CASE','merged')
if name=='gh':
 if case=='api-failure': sys.exit(1)
 print(json.dumps({'state': 'OPEN' if case=='open' else 'CLOSED' if case=='closed' else 'MERGED', 'headRefOid': os.environ['HEAD']}))
elif name=='tmux':
 if 'new-session' in sys.argv:
  print(json.dumps(sys.argv[1:]))
 elif 'list-sessions' in sys.argv: print('worker-fixture-test' if case in ('live','attached') else 'gaffer-fixture')
 elif 'list-panes' in sys.argv: print(os.environ['WORKTREE'] if case=='pane' else '/unrelated')
elif name=='lsof':
 if case=='probe-failure': sys.exit(1)
 print('p1\\nn'+(os.environ['WORKTREE']+'/src' if case=='process' else '/unrelated'))
'''
    for name in ('gh', 'tmux', 'lsof'):
        p = bin_dir / name
        p.write_text(stub)
        p.chmod(0o755)
    env.update(PATH=str(bin_dir) + ':' + env['PATH'], WORKTREE=str(second), HEAD=run('git', '-C', str(second), 'rev-parse', 'HEAD'))
    argv = json.loads(run('bash', str(launcher), 'worker', '--', 'tmux', 'new-session', '-d', '-s', 'worker-fixture-test', '-c', str(second), env=env))
    assert f'CARGO_TARGET_DIR={home}/.cache/cargo-target/acme--factory' in argv and ' hold ' in argv[-1]
    print('PASS: tmux receives target environment and session lease wrapper')
    # Commit generated lockfile so this fixture starts clean.
    run('git', '-C', str(second), 'add', 'Cargo.lock')
    run('git', '-C', str(second), 'commit', '-m', 'lock')
    env['HEAD'] = run('git', '-C', str(second), 'rev-parse', 'HEAD')
    ledger = home / 'worker.json'
    ledger.write_text(json.dumps(dict(session='worker-fixture-test', repo='acme/factory', pr=1, worktree=str(second))))
    pending = home / 'pending'
    config = home / 'config.toml'
    config.write_text('repo_scope=["acme/factory"]\n')
    cleaner = ROOT / 'scripts/factory-clean-worktrees.py'
    run('python3', str(cleaner), 'remember', str(ledger), str(pending), '', env=env)
    for case in ('open', 'closed', 'live', 'attached', 'pane', 'process', 'api-failure', 'probe-failure'):
        result = subprocess.run(['python3', str(cleaner), 'sweep', str(config), str(pending)], env=dict(env, CASE=case), capture_output=True)
        assert second.exists() and (pending/'worker.json').exists(), case
        assert result.returncode == (1 if case in ('api-failure','probe-failure') else 0), result.stderr
    for dirty in ('source.txt', 'other-cache/blob'):
        p = second / dirty
        p.parent.mkdir(exist_ok=True)
        p.write_text('preserve')
        run('python3', str(cleaner), 'sweep', str(config), str(pending), env=env)
        assert p.exists()
        p.unlink()
        if p.parent.name == 'other-cache': p.parent.rmdir()
    # Scope walls, changed HEAD, locked worktrees, and primary checkout guard.
    for content in ('repo_scope=["acme/lab"]\n', 'repo_scope=["acme/factory"]\nrepo_scope_excludes=["acme/*"]\n'):
        config.write_text(content)
        result = subprocess.run(['python3', str(cleaner), 'sweep', str(config), str(pending)], env=env, capture_output=True)
        assert result.returncode == 1 and second.exists()
    config.write_text('repo_scope=["acme/factory"]\n')
    result = run('python3', str(cleaner), 'sweep', str(config), str(pending), env=dict(env, HEAD='0'*40))
    assert second.exists() and 'not merged' in result
    run('git', '-C', str(repo), 'worktree', 'lock', str(second))
    result = subprocess.run(['python3', str(cleaner), 'sweep', str(config), str(pending)], env=env, capture_output=True)
    assert result.returncode == 1 and second.exists()
    run('git', '-C', str(repo), 'worktree', 'unlock', str(second))
    primary = home/'primary.json'
    primary.write_text(json.dumps(dict(session='primary', repo='acme/factory', pr=1, worktree=str(repo))))
    run('python3', str(cleaner), 'remember', str(primary), str(pending), '', env=env)
    assert not (pending/'primary.json').exists()
    (second/'target').mkdir()
    (second/'target/blob').write_text('disposable')
    run('python3', str(cleaner), 'sweep', str(config), str(pending), '--dry-run', env=env)
    assert second.exists()
    run('python3', str(cleaner), 'sweep', str(config), str(pending), env=env)
    assert not second.exists() and not (pending/'worker.json').exists()
    assert (home/'.cache/cargo-target/acme--factory').exists()
    print('PASS: merged cleanup removes source + ignored target; retains live/attached/dirty/open/closed-unmerged/probe failures; dry-run preserves all; shared cache retained')

    # Run the actual reaper in a copied fixture installation. Only its PATH
    # prefix is omitted so no host tmux/browser command can escape the stubs.
    installation = home/'installation'
    shutil.copytree(ROOT/'scripts', installation/'scripts')
    (installation/'factories').mkdir()
    (installation/'factories/fixture.toml').write_text('repo_scope=["acme/factory"]\nidle_minutes="1"\n')
    reaper = installation/'scripts/factory-reap.sh'
    reaper.write_text(reaper.read_text().replace('export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"', '# fixture PATH is isolated'))
    # Reuse the surviving first worktree, with the generated lockfile committed.
    run('git', '-C', str(first), 'add', 'Cargo.lock')
    run('git', '-C', str(first), 'commit', '-m', 'lock')
    ledgers, harvests = home/'ledgers', home/'harvests'
    ledgers.mkdir()
    live_ledger = ledgers/'worker-fixture-test.json'
    live_ledger.write_text(json.dumps(dict(session='worker-fixture-test', instance='fixture', repo='acme/factory', pr=1, worktree=str(first))))
    session_marker = home/'session-alive'
    tmux_stub = bin_dir/'tmux'
    tmux_stub.write_text("""#!/usr/bin/env python3
import os,sys,time
from pathlib import Path
args=sys.argv[1:]
marker=Path(os.environ['SESSION_MARKER'])
case=os.environ.get('CASE','open')
if args[0]=='list-sessions':
 if '#{window_activity}' in args[-1]:
  if marker.exists(): print('worker-fixture-test|0|'+('1' if case=='attached' else '0'))
 else: print('worker-fixture-test' if marker.exists() else 'gaffer-fixture')
elif args[0]=='has-session': sys.exit(0 if marker.exists() else 1)
elif args[0]=='display-message': print(os.environ['WORKTREE'] if args[-1]=='#{pane_current_path}' else '1')
elif args[0]=='capture-pane': print('fixture worker finished')
elif args[0]=='kill-session': marker.unlink()
elif args[0]=='list-panes': print('/unrelated')
""")
    env.update(SESSION_MARKER=str(session_marker), WORKTREE=str(first), HEAD=run('git','-C',str(first),'rev-parse','HEAD'), FACTORY_LEDGER_DIR=str(ledgers), FACTORY_HARVEST_DIR=str(harvests))
    session_marker.touch()
    run('bash', str(reaper), 'fixture', env=dict(env, CASE='attached'))
    assert session_marker.exists() and live_ledger.exists() and first.exists()
    run('bash', str(reaper), 'fixture', '--dry-run', env=dict(env, CASE='open'))
    assert session_marker.exists() and live_ledger.exists() and not harvests.exists()
    run('bash', str(reaper), 'fixture', env=dict(env, CASE='open'))
    candidate = harvests/'fixture/worktrees/worker-fixture-test.json'
    assert not session_marker.exists() and not live_ledger.exists() and candidate.exists() and first.exists()
    assert (harvests/'fixture/worker-fixture-test.log').exists()
    run('bash', str(reaper), 'fixture', env=dict(env, CASE='merged'))
    assert not first.exists() and not candidate.exists()
    print('PASS: reaper integration preserves attached/dry-run; harvest archives pane+ledger; later beat removes merged worktree after live ledger is gone')
