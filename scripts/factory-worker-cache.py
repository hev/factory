#!/usr/bin/env python3
"""Launch workers with a per-repository Cargo target and a maintenance lease."""
import fcntl
import os
from pathlib import Path
import re
import shlex
import signal
import subprocess
import sys


def run(args):
    return subprocess.check_output(args, text=True).strip()


def main(args):
    if args[0] == 'hold':
        cache = Path(args[1])
        locks = cache.parent / '.locks'
        locks.mkdir(parents=True, exist_ok=True)
        with (locks / cache.name).open('a') as lease:
            fcntl.flock(lease, fcntl.LOCK_SH)
            if cache.is_symlink():
                raise ValueError('worker target must not be a symlink')
            cache.mkdir(parents=True, exist_ok=True)
            os.utime(cache, None)
            # The interactive shell handles terminal interrupts. A caught
            # handler resets to default on child exec, unlike SIG_IGN.
            signal.signal(signal.SIGINT, lambda *_: None)
            signal.signal(signal.SIGQUIT, lambda *_: None)
            try:
                return subprocess.call(args[2:], env=dict(os.environ, CARGO_TARGET_DIR=str(cache)), pass_fds=(lease.fileno(),))
            finally:
                os.utime(cache, None)
    cwd = os.getcwd()
    tmux = Path(args[0]).name == 'tmux' and 'new-session' in args
    if tmux:
        # The contract launches an interactive shell, then submits the brief.
        # Reject shell-command variants rather than accidentally double wrapping.
        start = args.index('new-session')
        flags = args[start + 1:]
        i = 0
        while i < len(flags):
            flag = flags[i]
            if flag in ('-d', '-A', '-D', '-P', '-E'):
                i += 1
            elif flag in ('-s', '-t', '-n', '-c', '-e', '-F', '-x', '-y') and i + 1 < len(flags):
                if flag == '-c':
                    cwd = flags[i + 1]
                i += 2
            else:
                raise ValueError('worker tmux launch requires separate flags and no shell-command; submit the harness after launch')
    remote = run(['git', '-C', cwd, 'remote', 'get-url', 'origin'])
    match = re.fullmatch(r'(?:https://github.com/|git@github.com:)([\w.-]+)/([\w.-]+?)(?:\.git)?', remote)
    if not match:
        raise ValueError('worker origin must identify a GitHub owner/repository')
    key = '--'.join(match.groups()).lower()
    cache = Path.home() / '.cache/cargo-target' / key
    hold = [sys.executable, str(Path(__file__).resolve()), 'hold', str(cache)]
    if tmux:
        args[start + 1:start + 1] = ['-e', f'CARGO_TARGET_DIR={cache}']
        args += [shlex.join(hold + [os.environ.get('SHELL', '/bin/bash'), '-l'])]
        return subprocess.call(args)
    return subprocess.call(hold + args)


if __name__ == '__main__':
    try:
        sys.exit(main(sys.argv[1:]))
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print(f'worker cache: {error}', file=sys.stderr)
        sys.exit(1)
