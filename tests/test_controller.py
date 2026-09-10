import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('controller',Path(__file__).resolve().parents[1]/'scripts/factory-controller.py')
c=importlib.util.module_from_spec(spec);spec.loader.exec_module(c)

class ControllerTest(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(self.tmp.name);self.state=self.root/'state';self.base=self.state/'controller'
        self.cfg={'name':'acme','runtime':'sessions','home_host':'fixture','workspace_path':str(self.root/'workspace'),
                  'linear_team':'team','linear_approved_state':'Todo','repo_scope':['acme/app'],'linear_approval_actors':['human']}
        for obj,key,val in [(c,'STATE',self.state),(c,'BASE',self.base),(c.s,'STATE',self.state)]:
            p=patch.object(obj,key,val);p.start();self.addCleanup(p.stop)
        for p in [patch.object(c.s,'local_configs',return_value={'acme':self.cfg}),
                  patch.object(c.s,'configs',return_value={'acme':self.cfg}),
                  patch.dict(os.environ,FACTORY_HOSTNAME_OVERRIDE='fixture')]:
            p.start();self.addCleanup(p.stop)
        self.base.mkdir(parents=True)
        self.issue={'id':'ENG-1','url':'https://linear.app/acme/issue/ENG-1','description':'Do the work',
                    'createdById':'human','createdAt':'t0','updatedAt':'t1','labels':['rfc'],
                    'stateHistory':[{'state':{'name':'Todo'},'startedAt':'t0'}]}

    def record(self):
        r={'session':'gaffer-acme-eng-1','instance':'acme','plan':str(self.root/'plan.md'),
           'status':'running','transport':'exec','issue':'ENG-1'}
        c.s.write(self.state/'gaffers'/ (r['session']+'.json'),r);return r

    def test_duplicate_completed_event_never_reopens(self):
        p=c.event('gaffer-acme-one','key',{'data':1});e=c.read(p);e['status']='done';c.s.write(p,e)
        c.event('gaffer-acme-one','key',{'data':2})
        self.assertEqual(c.read(p)['status'],'done');self.assertEqual(c.read(p)['payload'],{'data':1})

    def test_initial_todo_attribution_and_later_transition(self):
        self.assertIsNone(c.approval(self.issue,self.cfg)[1])
        self.issue['stateHistory'][0]['startedAt']='t2'
        self.assertIsNotNone(c.approval(self.issue,self.cfg)[1])
        self.issue['stateHistory'][0]['actorId']='human'
        self.assertIsNone(c.approval(self.issue,self.cfg)[1])

    def test_receipt_is_bound_to_team_and_body(self):
        self.cfg['linear_approval_actors']=[]
        r={'team':'team','body_sha256':c.digest(self.issue['description']),'actor':'human'}
        c.s.write(self.base/'approvals/ENG-1.json',r)
        self.assertIsNone(c.approval(self.issue,self.cfg)[1])
        self.issue['description']='changed scope'
        self.assertIn('changed',c.approval(self.issue,self.cfg)[1])

    def test_intake_creates_one_owner_and_rejects_scope(self):
        with patch.object(c,'Linear') as linear:
            linear.return_value.approved.return_value=[self.issue]
            linear.return_value.call.return_value=self.issue
            self.assertEqual(c.intake('acme',self.cfg),[])
            self.assertEqual(c.intake('acme',self.cfg),[])
            self.assertEqual(len(c.s.records()),1)
        r={'team':'team','body_sha256':c.digest(self.issue['description']),'repos':['other/app']}
        c.s.write(self.base/'approvals/ENG-1.json',r)
        with patch.object(c,'Linear') as linear:
            linear.return_value.approved.return_value=[self.issue];linear.return_value.call.return_value=self.issue
            self.assertEqual(c.intake('acme',self.cfg)[0]['repos'],['other/app'])

    def test_unknown_actor_is_reported_without_assignment(self):
        self.cfg['linear_approval_actors']=[]
        with patch.object(c,'Linear') as linear:
            linear.return_value.approved.return_value=[self.issue];linear.return_value.call.return_value=self.issue
            self.assertIn('actor',c.intake('acme',self.cfg)[0]['reason'])
        self.assertEqual(c.s.records(),[])

    def test_per_assignment_lock_prevents_another_owner(self):
        with c.gate(self.base/'locks/test.lock'):
            self.assertTrue(c.active('test'))
        self.assertFalse(c.active('test'))

    def fake(self,script):
        return patch.object(c,'command',return_value=[sys.executable,'-c',script])

    def test_real_subprocess_acknowledges_and_completes(self):
        r=self.record();p=c.event(r['session'],'go',{})
        with self.fake('import sys,json;sys.stdin.read();print(json.dumps({"type":"turn.started"}),flush=True);print(json.dumps({"type":"turn.completed"}),flush=True)'):
            c.run_turn(r['session'])
        self.assertEqual(c.read(p)['status'],'done')
        self.assertTrue(c.read(self.base/'turns'/(r['session']+'.json'))['acknowledged'])

    def test_exit_zero_without_ack_is_failure_and_retries(self):
        r=self.record();p=c.event(r['session'],'go',{})
        with self.fake('import sys;sys.stdin.read()'):
            c.run_turn(r['session'])
        self.assertEqual(c.read(p)['status'],'pending')
        self.assertEqual(c.read(p)['attempts'],1)
        self.assertGreater(c.read(p)['not_before'],c.time.time())

    def test_abandoned_running_event_is_recovered(self):
        r=self.record();p=c.event(r['session'],'go',{});e=c.read(p);e['status']='running';c.s.write(p,e)
        with self.fake('import sys;sys.stdin.read();print(\'{"type":"turn.started"}\');print(\'{"type":"turn.completed"}\')'):
            c.run_turn(r['session'])
        self.assertEqual(c.read(p)['status'],'done')

    def test_failed_turn_blocks_after_three_attempts(self):
        r=self.record();p=c.event(r['session'],'go',{})
        with self.fake('import sys;sys.stdin.read();sys.exit(7)'):
            for _ in range(3):
                e=c.read(p);e['not_before']=0;c.s.write(p,e);c.run_turn(r['session'])
        self.assertEqual(c.read(p)['status'],'blocked')

    def test_hold_prevents_processing(self):
        r=self.record();p=c.event(r['session'],'go',{})
        h=self.state/'holds/acme';h.parent.mkdir();h.touch()
        with patch.object(c,'execute') as execute:c.run_turn(r['session']);execute.assert_not_called()
        self.assertEqual(c.read(p)['attempts'],0)

    def test_event_wake_never_reads_or_writes_composer(self):
        with patch.object(c.s,'events_enabled',return_value=True),patch.object(c.s,'controller',return_value=c),patch.object(c,'spawn'),patch.object(c.s,'run') as terminal:
            c.s.wake('foreman','literal $()\nmessage')
            terminal.assert_not_called()
        self.assertEqual(len(list((self.base/'queues/foreman').glob('*.json'))),1)

    def test_model_inherits_fence_when_wrapper_is_killed(self):
        lock=self.base/'locks/crash.lock'
        helper='''import fcntl,subprocess,sys,os,time
f=open(sys.argv[1],'a');fcntl.flock(f,fcntl.LOCK_EX)
p=subprocess.Popen([sys.executable,'-c','import time;time.sleep(2)'],pass_fds=[f.fileno()])
print(p.pid,flush=True)
os._exit(0)
'''
        lock.parent.mkdir(parents=True,exist_ok=True)
        p=subprocess.Popen([sys.executable,'-c',helper,str(lock)],stdout=subprocess.PIPE,text=True)
        child=int(p.stdout.readline());p.wait();p.stdout.close()
        self.assertTrue(c.active('crash'))
        os.kill(child,15)

    def test_start_timeout_requeues_without_manual_input(self):
        r=self.record();p=c.event(r['session'],'go',{})
        with patch.dict(os.environ,FACTORY_START_TIMEOUT='1'),self.fake('import sys,time;sys.stdin.read();time.sleep(30)'):
            c.run_turn(r['session'])
        self.assertEqual(c.read(p)['status'],'pending')
        self.assertEqual(c.read(self.base/'turns'/(r['session']+'.json'))['error'],'TimeoutError')

if __name__=='__main__':unittest.main()
