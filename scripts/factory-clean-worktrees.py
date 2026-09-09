#!/usr/bin/env python3
"""Retain harvest candidates until a scoped PR is merged and removal is safe."""
import fnmatch
import json
from pathlib import Path
import re
import subprocess
import sys
try:
    import tomllib
except ImportError:
    sys.exit('cleanup: Python 3.11+ is required; no worktrees removed')


def command(*args):
    result = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
    if args[0] == 'lsof' and result.stderr.strip():
        raise ValueError('process probe incomplete: ' + result.stderr.strip())
    return result.stdout.strip()


def remember(ledger, directory, cwd):
    data = json.loads(Path(ledger).read_text())
    path = data.get('worktree') or cwd
    if not path or not all(data.get(key) for key in ('pr', 'repo', 'session')):
        return
    # Only registered linked worktrees can become candidates, never a checkout.
    path = Path(command('git', '-C', path, 'rev-parse', '--show-toplevel')).resolve()
    if not (path / '.git').is_file():
        return
    data['worktree'] = str(path)
    data['head'] = command('git', '-C', str(path), 'rev-parse', 'HEAD')
    directory.mkdir(parents=True, exist_ok=True)
    target = directory / Path(ledger).name
    temporary = target.with_suffix('.tmp')
    temporary.write_text(json.dumps(data) + '\n')
    temporary.replace(target)


def sweep(config, directory, dry):
    settings = tomllib.loads(Path(config).read_text())
    scope = settings.get('repo_scope', [])
    excludes = settings.get('repo_scope_excludes', [])
    failures = False
    for record in sorted(directory.glob('*.json')):
        try:
            data = json.loads(record.read_text())
            repo, pr = data['repo'], str(data['pr'])
            if repo not in scope or any(fnmatch.fnmatchcase(repo, pattern) for pattern in excludes) or not re.fullmatch(r'[\w.-]+/[\w.-]+', repo) or not pr.isdigit():
                raise ValueError('candidate outside repo_scope or invalid PR')
            path = Path(data['worktree'])
            session = data['session']
            sessions = command('tmux', 'list-sessions', '-F', '#{session_name}')
            if session in sessions.splitlines():
                print(f'kept {path}: session exists')
                continue
            if not path.exists():
                if not dry:
                    record.unlink()
                continue
            if path.is_symlink() or path.resolve() != path or not (path / '.git').is_file():
                raise ValueError('not a canonical linked worktree')
            origin = command('git', '-C', str(path), 'remote', 'get-url', 'origin')
            if origin.removesuffix('.git') not in (f'https://github.com/{repo}', f'git@github.com:{repo}'):
                raise ValueError('origin differs from ledger repo')
            state = json.loads(command('gh', 'pr', 'view', pr, '--repo', repo, '--json', 'state,headRefOid'))
            head = command('git', '-C', str(path), 'rev-parse', 'HEAD')
            if state['state'] != 'MERGED' or head != state['headRefOid'] or head != data['head']:
                print(f'kept {path}: PR not merged at recorded HEAD')
                continue
            # Include ignored files: only ignored Cargo target output is disposable.
            status = command('git', '-C', str(path), 'status', '--porcelain=v1', '--ignored', '--untracked-files=all')
            if any(not line.startswith('!! target/') for line in status.splitlines()):
                print(f'kept {path}: dirty or unrelated ignored files')
                continue
            if (path / 'target').is_symlink():
                raise ValueError('target is a symlink')
            # Check every pane and every observable process file/cwd, including children
            # of workers that have already lost their session. Probe failure retains.
            panes = command('tmux', 'list-panes', '-a', '-F', '#{pane_current_path}')
            cwd_output = command('lsof', '-nP', '-Fn')
            paths = panes.splitlines() + [line[1:] for line in cwd_output.splitlines() if line.startswith('n')]
            if any(Path(p).is_absolute() and (Path(p).resolve() == path or path in Path(p).resolve().parents) for p in paths):
                print(f'kept {path}: live pane/process file or cwd')
                continue
            # Git also refuses locked worktrees. No branch deletion or shared cache
            # deletion: those artifacts have independent lifetimes.
            if dry:
                print(f'would remove {path}')
            else:
                subprocess.run(['git', '-C', str(path.parent), '--git-dir', command('git', '-C', str(path), 'rev-parse', '--path-format=absolute', '--git-common-dir'), 'worktree', 'remove', '--force', str(path)], check=True)
                record.unlink()
                print(f'removed {path}: merged PR #{pr}')
        except (KeyError, ValueError, OSError, subprocess.SubprocessError) as error:
            failures = True
            print(f'cleanup retained {record.name}: {error}', file=sys.stderr)
    return int(failures)


if __name__ == '__main__':
    try:
        if sys.argv[1] == 'remember':
            remember(sys.argv[2], Path(sys.argv[3]), sys.argv[4])
        else:
            sys.exit(sweep(sys.argv[2], Path(sys.argv[3]), '--dry-run' in sys.argv[4:]))
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print(f'cleanup: {error}', file=sys.stderr)
        sys.exit(1)
