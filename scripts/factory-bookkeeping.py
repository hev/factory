#!/usr/bin/env python3
"""Copy a pinned approved plan through a dedicated, reviewable Git branch."""
import hashlib
import json
from pathlib import Path


def prepare(c, session, lane, publish=False):
    with c.gate(c.BASE / 'bookkeeping' / (c.s.name(session) + '.lock')):
        record, key = c.dispatch.acknowledged_owner(c, session)
        cfg = c.s.local_configs()[record['instance']]
        if not cfg.get('linear_team') or not record.get('approval'):
            raise ValueError('bookkeeping requires an approved Linear assignment')
        if (record['approval'].get('team') != cfg['linear_team'] or
                not record['approval'].get('actor') or
                record.get('owner', session) != session):
            raise ValueError('bookkeeping approval team, actor or owner mismatch')
        if record.get('transport') != 'exec' or record['status'] == 'retired':
            raise ValueError('bookkeeping requires an active exec assignment')
        if (c.STATE / 'winddown' / record['instance']).exists():
            raise ValueError('bookkeeping cannot start during winddown')
        source = Path(record['plan'])
        content = source.read_bytes()
        sha = hashlib.sha256(content).hexdigest()
        if not record.get('approved_plan_sha256') or sha != record['approved_plan_sha256']:
            raise ValueError('approved plan bytes changed or lack an intake digest; owner recovery required')
        if not record.get('source') or not content.startswith(('> Approved source: ' + record['source'] + '\n').encode()):
            raise ValueError('approved plan source does not match assignment')
        repo, base = cfg['plans_repo'], cfg.get('plans_branch', 'main')
        if not c.dispatch.in_scope(cfg, repo):
            raise ValueError('plans repository outside scope')
        lane = Path(lane)
        c.dispatch.validate_lane(c, dict(repo=repo, worktree=str(lane), brief=str(source)))
        for other in c.s.records():
            if other['status'] != 'retired' and any(c.dispatch.overlap(lane, p) for p in other.get('worktree_lanes', {})):
                raise ValueError('bookkeeping needs a dedicated lane with no commissioned workers')
        if any(c.read(p).get('worktree') and c.dispatch.overlap(lane, c.read(p)['worktree'])
               for p in c.dispatch.ledger_dir(c).glob('*.json')):
            raise ValueError('bookkeeping lane has a worker ledger')

        def git(*args):
            return c.s.run('git', '-C', lane, *args).stdout.strip()

        branch = 'bookkeeping/' + session
        git('check-ref-format', 'refs/heads/' + base)
        if git('branch', '--show-current') != branch or branch == base:
            raise ValueError('bookkeeping lane must use branch ' + branch)
        path = 'plans/active/' + source.name
        receipt_path = c.BASE / 'bookkeeping' / (session + '.json')
        receipt = c.read(receipt_path)
        if receipt:
            expected = dict(repo=repo, base=base, branch=branch, lane=str(lane), path=path, sha256=sha)
            if any(receipt.get(k) != v for k, v in expected.items()):
                raise ValueError('bookkeeping provenance changed; preserve the original attempt')
        else:
            base_head = git('rev-parse', '--verify', 'refs/remotes/origin/' + base)
            if git('rev-parse', 'HEAD') != base_head or git('status', '--porcelain'):
                raise ValueError('new bookkeeping lane must be clean at origin/' + base)
            receipt = dict(repo=repo, base=base, base_head=base_head, branch=branch,
                           lane=str(lane), path=path, sha256=sha, event=key, source=record['source'])
            c.s.write(receipt_path, receipt)  # durable intent before any Git mutation
        # Reject extra staged, unstaged or committed paths on recovery. Only an
        # interrupted exact copy is resumable; no reset, stash or force push.
        git('merge-base', '--is-ancestor', receipt['base_head'], 'HEAD')
        if int(git('rev-list', '--count', receipt['base_head'] + '..HEAD')) > 1:
            raise ValueError('bookkeeping branch contains additional commits')
        changed = set(git('diff', '--name-only', receipt['base_head']).splitlines())
        changed.update(git('ls-files', '--others', '--exclude-standard').splitlines())
        changed.update(git('diff', '--cached', '--name-only').splitlines())
        if changed - {path}:
            raise ValueError('bookkeeping lane contains unrelated changes')
        target = lane / path
        # Neither a tracked symlink nor a symlinked parent may escape the lane.
        if target.resolve() != target or lane not in target.resolve().parents:
            raise ValueError('bookkeeping target must be a regular path inside its lane')
        if target.exists() and target.read_bytes() != content:
            raise ValueError('bookkeeping target already has different bytes')
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(content)
        git('add', '--', path)
        if git('diff', '--cached', '--name-only'):
            git('commit', '-m', 'Record approved plan ' + source.stem + '\n\nApproved-source: ' + record['source'] + '\nPlan-SHA256: ' + sha)
        if git('status', '--porcelain') or hashlib.sha256(target.read_bytes()).hexdigest() != sha:
            raise ValueError('bookkeeping verification failed')
        changed = set(git('diff', '--name-only', receipt['base_head'], 'HEAD').splitlines())
        if changed - {path}:
            raise ValueError('bookkeeping commit contains unrelated paths')
        # Git attributes/clean filters and commit hooks can change the index
        # while leaving the working copy looking exact. Verify the stored blob.
        if (git('rev-parse', 'HEAD:' + path) != git('hash-object', '--no-filters', '--', path) or
                not git('ls-tree', 'HEAD', '--', path).startswith('100644 blob ')):
            raise ValueError('committed plan bytes or file mode differ from approved copy')
        receipt.update(head=git('rev-parse', 'HEAD'), verified_at=c.s.stamp(), status='prepared')
        if not changed:
            receipt['status'] = 'already-present'
        c.s.write(receipt_path, receipt)
        if publish and receipt['status'] != 'already-present':
            # Recheck gates immediately before external writes. The existing
            # identity executable remains the complete authentication seam.
            c.dispatch.acknowledged_owner(c, session)
            if (c.STATE / 'winddown' / record['instance']).exists():
                raise ValueError('bookkeeping publication stopped by winddown')

            def as_gaffer(*args):
                return c.s.run(c.ROOT / 'scripts/factory-as.sh', 'gaffer', '--', *args).stdout.strip()

            prs = json.loads(as_gaffer('gh', 'pr', 'list', '--repo', repo, '--head', branch,
                                     '--base', base, '--state', 'all', '--json', 'url,state,headRefOid'))
            if len(prs) > 1 or (prs and (prs[0]['headRefOid'] != receipt['head'] or prs[0]['state'] == 'CLOSED')):
                raise ValueError('existing bookkeeping PR requires owner recovery')
            if not prs or prs[0]['state'] != 'MERGED':
                as_gaffer('git', '-C', lane, 'push', 'origin', 'HEAD:refs/heads/' + branch)
            if prs:
                url = prs[0]['url']
            else:
                body = receipt_path.with_suffix('.md')
                body.write_text('Approved source: ' + record['source'] + '\n\nPlan SHA256: `' + sha +
                                '`\n\nVerified byte-for-byte copy; only `' + path + '` changed.\n' +
                                'Publication records approval; merge and CI remain subject to existing gates.\n')
                url = as_gaffer('gh', 'pr', 'create', '--repo', repo, '--head', branch, '--base', base,
                                '--title', '[docs] Record approved plan ' + source.stem, '--body-file', body)
            receipt.update(status='published', pr=url)
            c.s.write(receipt_path, receipt)
        return receipt
