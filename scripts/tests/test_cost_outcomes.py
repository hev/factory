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
                          'merge_commit_sha':'abc','html_url':'https://example.com/pr/1'}])
        if endpoint=='deployments': return iter([])
        if endpoint.startswith('actions/runs?'):
            return iter([{'id':1,'path':'.github/workflows/ci.yml','event':'pull_request','conclusion':'success','head_sha':'abc','updated_at':'2026-09-08T00:00:00Z'},
                         {'id':2,'path':'.github/workflows/ci.yml','event':'push','conclusion':'success','head_sha':'abc','updated_at':'2026-09-08T00:00:00Z','html_url':'https://example.com/run/2'}])
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

if __name__=='__main__': unittest.main()
