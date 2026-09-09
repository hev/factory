#!/usr/bin/env python3
"""Build kit's content-free outcomes cache from a scoped issue snapshot and GitHub."""
import argparse
import datetime as dt
import json
from pathlib import Path
import re
import subprocess
import sys
try:
    import tomllib
except ImportError:
    tomllib = None
from ingest import atomic_write, stamp


def config(path):
    if tomllib is None:
        raise ValueError('outcome refresh requires Python 3.11+ for TOML config')
    c = tomllib.loads(path.read_text())
    team, repos = c.get('linear_team'), c.get('repo_scope', [])
    if not team or not repos or any(not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', r) for r in repos):
        raise ValueError('linear_team and explicit owner/repository repo_scope required')
    return team, repos


class GitHub:
    def __init__(self, repos):
        self.repos = set(repos)

    def get(self, repo, suffix):
        if repo not in self.repos or not re.fullmatch(r'(?:(pulls|deployments|actions/runs)(/[0-9]+/(statuses|jobs))?(?:\?[^#]*)?|compare/[0-9a-f]{40}\.\.\.[0-9a-f]{40})', suffix):
            raise ValueError('GitHub request outside explicit repository scope')
        p = subprocess.run(['gh', 'api', '--method', 'GET', f'repos/{repo}/{suffix}'], capture_output=True, text=True)
        if p.returncode:
            # Error bodies and headers may contain identities. Keep diagnostics scoped.
            raise ValueError(f'GitHub GET failed for {repo}/{suffix.split("?")[0]} (exit {p.returncode})')
        return json.loads(p.stdout)

    def pages(self, repo, endpoint, key=None):
        page = 1
        while True:
            sep = '&' if '?' in endpoint else '?'
            data = self.get(repo, f'{endpoint}{sep}per_page=100&page={page}')
            if endpoint.startswith('actions/runs?') and data.get('total_count', 0) > 1000:
                raise ValueError('GitHub filtered workflow history exceeds 1000-result cap; partition source history before claiming complete coverage')
            rows = data[key] if key else data
            yield from rows
            if len(rows) < 100:
                break
            page += 1


def build(snapshot, team, repos, github, since_ms, workflow_paths=None):
    if snapshot.get('team') != team:
        raise ValueError('issue snapshot team differs from configured linear_team')
    now = int(dt.datetime.now(dt.timezone.utc).timestamp()*1000)
    source_time = snapshot.get('refreshed_at', 0)
    if not isinstance(source_time, int) or source_time <= 0 or source_time > now:
        raise ValueError('issue snapshot needs a valid refreshed_at timestamp')
    issues = {}
    for source in snapshot.get('issues', []):
        key = source.get('id') or source.get('issue')
        if not key or not re.fullmatch(r'[A-Za-z][A-Za-z0-9]*-[0-9]+', key) or key in issues or source.get('team', team) != team:
            raise ValueError('invalid, duplicate or out-of-team issue')
        issues[key] = dict(issue=key, team=team, state=source.get('status', ''),
                           done_at=stamp(source.get('completedAt')), prs=[], deploys=[])
    diagnostics = []
    all_deploys = []
    all_prs = []
    for repo in repos:
        by_sha = {}
        pulls = []
        comparisons = {}
        unassociated = []
        seen_prs = set()
        seen_deploys = set()
        scanned_prs = 0
        verified_deploys = 0
        associated_deploys = 0

        def associate(row):
            nonlocal verified_deploys, associated_deploys
            identity = (row['source'], row['id'])
            if identity in seen_deploys:
                return
            seen_deploys.add(identity)
            verified_deploys += 1
            all_deploys.append(row)
            matched = {}
            for merge_sha, candidates in by_sha.items():
                eligible = {key for key, merged_at in candidates if merged_at <= row['landed_at']}
                if not eligible:
                    continue
                head = row['sha']
                if not re.fullmatch(r'[0-9a-f]{40}', head or ''):
                    raise ValueError('deployment requires a full commit SHA')
                pair = (merge_sha, head)
                if pair not in comparisons:
                    comparisons[pair] = ('identical' if merge_sha == head else
                        github.get(repo, f'compare/{merge_sha}...{head}').get('status'))
                status = comparisons[pair]
                if status not in ('identical', 'ahead', 'behind', 'diverged'):
                    raise ValueError('unrecognized GitHub ancestry comparison')
                if status in ('identical', 'ahead'):
                    for key in eligible:
                        matched.setdefault(key, []).append(dict(merge_sha=merge_sha, relation=status))
            for key, proofs in matched.items():
                issues[key]['deploys'].append(dict(row, association='verified_ancestry', merge_proofs=proofs))
            associated_deploys += bool(matched)
            if not matched:
                unassociated.append(dict(row, reason='no explicitly referenced merged ancestor before landing'))
        for pr in github.pages(repo, 'pulls?state=all&sort=updated&direction=desc'):
            if pr['number'] in seen_prs:
                continue
            seen_prs.add(pr['number'])
            scanned_prs += 1
            # Scan all PR pages: an older merge can ship during this window.
            # Exact known identifiers explicitly referenced by the PR. No title
            # similarity, branch-name guessing or account-global search.
            refs = set(re.findall(r'(?<![A-Za-z0-9-])[A-Za-z][A-Za-z0-9]*-[0-9]+(?![0-9])',
                                  (pr.get('title') or '')+'\n'+(pr.get('body') or ''))) & issues.keys()
            row = dict(url=pr['html_url'], repo=repo, number=pr['number'], merged_at=stamp(pr.get('merged_at')),
                       association='explicit_issue_reference' if refs else 'unassociated')
            all_prs.append(row)
            if not refs:
                continue
            for key in refs:
                issues[key]['prs'].append(row)
            if pr.get('merged_at') and pr.get('merge_commit_sha'):
                if not re.fullmatch(r'[0-9a-f]{40}', pr['merge_commit_sha']):
                    raise ValueError('merged PR requires a full commit SHA')
                by_sha.setdefault(pr['merge_commit_sha'], set()).update((key, stamp(pr['merged_at'])) for key in refs)
            pulls.append(pr)
        # A successful test/build is not a deployment. Production GitHub
        # deployments require an actual success status; CI deploy workflows must
        # be explicitly named in operator config (exact repository/path pairs).
        for deploy in github.pages(repo, 'deployments'):
            # A deployment created earlier can acquire a success in the window.
            if not deploy.get('production_environment', deploy.get('environment') == 'production'):
                continue
            statuses = list(github.pages(repo, f'deployments/{deploy["id"]}/statuses'))
            success = [s for s in statuses if s.get('state') == 'success']
            if not success:
                continue
            status = max(success, key=lambda s: s.get('created_at', ''))
            row = dict(id=str(deploy['id']), repo=repo, url=status.get('environment_url') or deploy.get('url', ''),
                       landed_at=stamp(status.get('created_at')), source='github_deployment', sha=deploy['sha'])
            if row['landed_at'] >= since_ms:
                associate(row)
        paths = (workflow_paths or {}).get(repo, [])
        if paths:
            for run in github.pages(repo, 'actions/runs?status=success', 'workflow_runs'):
                if stamp(run.get('updated_at')) < since_ms:
                    continue
                selected = [x for x in paths if x['path'] == run.get('path')]
                if not selected or run.get('conclusion') != 'success' or run.get('event') not in ('push','workflow_dispatch','release'):
                    continue
                jobs = list(github.pages(repo, f'actions/runs/{run["id"]}/jobs', 'jobs'))
                landed = [job for job in jobs if job.get('conclusion') == 'success' and any(x['job'] == job.get('name') for x in selected)]
                if not landed: continue
                landed_at = max(stamp(job.get('completed_at')) for job in landed)
                if landed_at >= since_ms:
                    associate(dict(id='run-'+str(run['id']), repo=repo, url=run['html_url'],
                        landed_at=landed_at, source='configured_deploy_job', sha=run['head_sha']))
        diagnostics.append(dict(repo=repo, referenced_prs=len(pulls), deploy_workflows=paths,
            pr_history='all_pages', scanned_prs=scanned_prs, merged_shas=len(by_sha), verified_deploys=verified_deploys, associated_deploys=associated_deploys,
            unassociated_deploys=verified_deploys-associated_deploys, unassociated_deployments=unassociated,
            ancestry_comparisons=len(comparisons), ancestry_results=[dict(merge_sha=base, head_sha=head, relation=status)
                for (base, head), status in sorted(comparisons.items())]))
    # Refreshing GitHub must not make an old issue snapshot look fresh.
    return dict(schema_version=1, team=team, refreshed_at=min(source_time, now), github_refreshed_at=now,
                coverage_start=since_ms, issues=list(issues.values()), prs=all_prs, deploys=all_deploys, repositories=diagnostics)


def validate_cache(cache, team, repos):
    if not isinstance(cache, dict) or not isinstance(cache.get('issues'), list):
        raise ValueError('outcome cache requires an issues array')
    if cache.get('schema_version') != 1 or cache.get('team') != team:
        raise ValueError('outcome cache schema/team mismatch')
    for key in ('refreshed_at','github_refreshed_at','coverage_start'):
        if type(cache.get(key)) is not int or cache[key] < 0:
            raise ValueError('invalid outcome timestamp')
    for record in cache.get('prs', []):
        if not isinstance(record, dict) or record.get('repo') not in repos or type(record.get('merged_at')) is not int or record['merged_at'] < 0 or not isinstance(record.get('url'), str):
            raise ValueError('invalid or out-of-scope global PR')
    for record in cache.get('deploys', []):
        if not isinstance(record, dict) or record.get('repo') not in repos or type(record.get('landed_at')) is not int or record['landed_at'] < 0 or not isinstance(record.get('id'), str) or record.get('source') not in ('github_deployment','configured_deploy_job'):
            raise ValueError('invalid or out-of-scope global deployment')
    seen = set()
    for issue in cache.get('issues', []):
        if not isinstance(issue, dict) or issue.get('team') != team or not isinstance(issue.get('issue'), str) or not issue['issue'] or issue['issue'] in seen or not isinstance(issue.get('state'), str):
            raise ValueError('invalid or duplicate outcome issue')
        seen.add(issue['issue'])
        if type(issue.get('done_at')) is not int or issue['done_at'] < 0:
            raise ValueError('invalid completion timestamp')
        for kind, stamp_key in (('prs','merged_at'),('deploys','landed_at')):
            if not isinstance(issue.get(kind), list):
                raise ValueError('outcome issue requires PR and deploy arrays')
            for record in issue[kind]:
                if not isinstance(record, dict) or record.get('repo') not in repos or type(record.get(stamp_key)) is not int or record[stamp_key] < 0:
                    raise ValueError('outcome record outside scope or invalid timestamp')
                if kind == 'prs' and not isinstance(record.get('url'), str):
                    raise ValueError('outcome PR requires a URL')
                if kind == 'deploys' and (not isinstance(record.get('id'), str) or record.get('source') not in ('github_deployment','configured_deploy_job')):
                    raise ValueError('outcome deploy requires an ID and verified source')
    return cache


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--config', type=Path, required=True)
    p.add_argument('--issues', type=Path)
    p.add_argument('--output', type=Path)
    p.add_argument('--validate-cache', type=Path, help='Validate an existing cache without any network request')
    p.add_argument('--days', type=int, default=30)
    p.add_argument('--deploy-workflows', type=Path, help='JSON map of repo to exact verified workflow path/job objects')
    a = p.parse_args()
    team, repos = config(a.config)
    if a.validate_cache:
        validate_cache(json.loads(a.validate_cache.read_text()),team,repos)
        print('outcome cache schema and scope valid')
        return
    if not a.issues or not a.output: p.error('--issues and --output are required for refresh')
    if a.days <= 0:
        p.error('--days must be positive')
    workflows = json.loads(a.deploy_workflows.read_text()) if a.deploy_workflows else {}
    if any(repo not in repos or not isinstance(paths, list) or any(not isinstance(x, dict) or not isinstance(x.get('path'),str) or not x['path'].startswith('.github/workflows/') or not isinstance(x.get('job'),str) or not x['job'] for x in paths) for repo, paths in workflows.items()):
        p.error('deployment workflow map must stay inside configured repo_scope')
    cutoff = int((dt.datetime.now(dt.timezone.utc)-dt.timedelta(days=a.days)).timestamp()*1000)
    result = build(json.loads(a.issues.read_text()), team, repos, GitHub(repos), cutoff, workflows)
    validate_cache(result,team,repos)
    a.output.parent.mkdir(parents=True, exist_ok=True)
    atomic_write(a.output, json.dumps(result, sort_keys=True)+'\n')
    print(json.dumps({'issues':len(result['issues']), 'prs':sum(len(i['prs']) for i in result['issues']),
                      'deploys':sum(len(i['deploys']) for i in result['issues'])}))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError) as e:
        print(str(e), file=sys.stderr)
        sys.exit(1)
