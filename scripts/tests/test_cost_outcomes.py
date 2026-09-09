import datetime as dt
import json
from pathlib import Path
import sys
import unittest
sys.path.insert(0, str(Path(__file__).parents[1]/'costs'))
from outcomes import build, GitHub, validate_cache
from attribution import Evidence

class FixtureGitHub:
    def __init__(self): self.requests=[]
    def pages(self, repo, endpoint, key=None):
        self.requests.append((repo,endpoint))
        if repo!='example/repo': raise AssertionError('scope escape')
        if endpoint.startswith('pulls'):
            return iter([{'number':1,'title':'EX-1 implementation','body':'EX-10 is unrelated',
                          'updated_at':'2026-09-08T00:00:00Z','merged_at':'2026-09-08T00:00:00Z',
                          'merge_commit_sha':'a'*40,'html_url':'https://example.com/pr/1'}])
        if endpoint=='deployments': return iter([])
        if endpoint.startswith('actions/runs?'):
            return iter([{'id':1,'path':'.github/workflows/ci.yml','event':'pull_request','conclusion':'success','head_sha':'a'*40,'updated_at':'2026-09-08T00:00:00Z'},
                         {'id':2,'path':'.github/workflows/ci.yml','event':'push','conclusion':'success','head_sha':'a'*40,'updated_at':'2026-09-08T00:00:00Z','html_url':'https://example.com/run/2'}])
        if endpoint=='actions/runs/2/jobs': return iter([{'name':'deploy','conclusion':'success','completed_at':'2026-09-08T00:01:00Z'}])
        raise AssertionError(endpoint)

class Outcomes(unittest.TestCase):
    def test_real_deploy_job_and_stale_issue_snapshot(self):
        source={'team':'example','refreshed_at':1,'issues':[{'id':'EX-1','status':'Done','completedAt':'2026-09-08T00:00:00Z'}]}
        api=FixtureGitHub()
        result=build(source,'example',['example/repo'],api,0,{'example/repo':[{'path':'.github/workflows/ci.yml','job':'deploy'}]})
        validate_cache(result,'example',['example/repo'])
        self.assertEqual(result['refreshed_at'],1)
        self.assertEqual(len(result['issues'][0]['prs']),1)
        self.assertEqual(len(result['issues'][0]['deploys']),1)
        self.assertNotIn(('example/repo','actions/runs/1/jobs'),api.requests)

    def test_scope_rejected_before_network(self):
        with self.assertRaises(ValueError): build({'team':'sibling'},'example',['example/repo'],FixtureGitHub(),0)
        with self.assertRaises(ValueError): GitHub(['example/repo']).get('sibling/repo','pulls')
        with self.assertRaises(ValueError): GitHub(['example/repo']).get('example/repo','../../notifications')

    def test_only_explicit_assignment_documents(self):
        e=Evidence()
        e.read({'payload':{'type':'message','role':'user','content':[{'text':'Read /home/u/.factory/briefs/example/task.md'}]}})
        e.read({'payload':{'type':'function_call','call_id':'c','arguments':'cat /home/u/.factory/briefs/example/task.md'}})
        e.read({'payload':{'type':'function_call_output','call_id':'unrelated','output':'Human RFC: EX-9'}})
        e.read({'payload':{'type':'function_call_output','call_id':'c','output':'Human RFC: https://linear.app/example/issue/EX-1\nRelated work EX-9'}})
        self.assertEqual(e.result()['issue'],'EX-1')
        self.assertEqual(e.result()['instance'],'example')

    def test_conflicting_assignments_are_not_invented(self):
        e=Evidence()
        e.read({'payload':{'type':'message','role':'user','content':[{'text':'Issue: EX-1\nIssue: EX-2'}]}})
        self.assertNotIn('issue',e.result())
        self.assertEqual(e.result()['issue_candidates'],['EX-1','EX-2'])


class Ancestry(unittest.TestCase):
    def test_older_merge_and_diverged_release(self):
        class API(FixtureGitHub):
            def pages(self,repo,endpoint,key=None):
                rows=list(super().pages(repo,endpoint,key))
                if endpoint.startswith('pulls'):
                    rows[0]['updated_at']='2020-01-01T00:00:00Z'
                    rows[0]['merged_at']='2020-01-01T00:00:00Z'
                if endpoint.startswith('actions/runs?'):
                    rows[1]['head_sha']='b'*40
                return iter(rows)
            def get(self,repo,endpoint):
                self.requests.append((repo,endpoint))
                return {'status':self.status}
        api=API()
        source={'team':'example','refreshed_at':1,'issues':[{'id':'EX-1','status':'Done'}]}
        for status,count in [('ahead',1),('behind',0),('diverged',0)]:
            api.status=status
            result=build(source,'example',['example/repo'],api,1,{'example/repo':[{'path':'.github/workflows/ci.yml','job':'deploy'}]})
            self.assertEqual(len(result['issues'][0]['deploys']),count)
            self.assertEqual(result['repositories'][0]['unassociated_deploys'],1-count)
            if count:
                self.assertEqual(result['issues'][0]['deploys'][0]['merge_proofs'],[{'merge_sha':'a'*40,'relation':'ahead'}])
        api.status='unknown'
        with self.assertRaises(ValueError):
            build(source,'example',['example/repo'],api,1,{'example/repo':[{'path':'.github/workflows/ci.yml','job':'deploy'}]})


class Coverage(unittest.TestCase):
    def test_old_deployment_new_success_and_future_merge(self):
        class API(FixtureGitHub):
            def pages(self,repo,endpoint,key=None):
                if endpoint=='deployments':
                    return iter([{'id':5,'created_at':'2020-01-01T00:00:00Z','sha':'a'*40,'production_environment':True}]*2)
                if endpoint=='deployments/5/statuses':
                    return iter([{'state':'success','created_at':'2026-09-08T00:01:00Z'}])
                rows=list(super().pages(repo,endpoint,key))
                if endpoint.startswith('pulls') and self.future:
                    rows[0]['merged_at']='2026-09-09T00:00:00Z'
                return iter(rows)
        api=API()
        source={'team':'example','refreshed_at':1,'issues':[{'id':'EX-1','status':'Done'}]}
        for future,count in [(False,1),(True,0)]:
            api.future=future
            result=build(source,'example',['example/repo'],api,100000)
            self.assertEqual(len(result['issues'][0]['deploys']),count)
            self.assertEqual(result['repositories'][0]['verified_deploys'],1)
            self.assertEqual(len(result['deploys']),1)
            validate_cache(result,'example',['example/repo'])
            result['deploys'][0]['repo']='outside/repo'
            with self.assertRaises(ValueError): validate_cache(result,'example',['example/repo'])

    def test_filtered_workflow_cap_fails_loudly(self):
        class API(GitHub):
            def get(self,*args): return {'total_count':1001,'workflow_runs':[]}
        with self.assertRaises(ValueError):
            list(API(['example/repo']).pages('example/repo','actions/runs?status=success','workflow_runs'))

if __name__=='__main__': unittest.main()
