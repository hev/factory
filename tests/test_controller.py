import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('controller',Path(__file__).resolve().parents[1]/'scripts/factory-controller.py')
c=importlib.util.module_from_spec(spec);spec.loader.exec_module(c)

class ControllerTest(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(self.tmp.name);self.state=self.root/'state';self.base=self.state/'controller'
        self.cfg={'name':'acme','runtime':'sessions','home_host':'fixture','workspace_path':str(self.root/'workspace'),
                  'linear_team':'team','linear_approved_state':'Todo','repo_scope':['acme/app'],'linear_approval_actors':['human'],'harness':'codex'}
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

    def test_plan_source_reads_either_declaration(self):
        plan=self.root/'p.md'
        plan.write_text('> Approved source: https://x/ENG-1\n\nbody\n')
        self.assertEqual(c.plan_source(plan),'https://x/ENG-1')
        plan.write_text('> Source RFC: https://x/ENG-2\n> Approval: whoever\n')
        self.assertEqual(c.plan_source(plan),'https://x/ENG-2')
        plan.write_text('no declaration, but mentions https://x/ENG-3\n')
        self.assertIsNone(c.plan_source(plan))
        self.assertIsNone(c.plan_source(self.root/'missing.md'))

    def test_sibling_link_does_not_adopt_another_assignment(self):
        # A plan body is its issue description verbatim, so linking related
        # issues is routine. The linked sibling must still get its own manager.
        other=self.root/'ticket-eng-9.md'
        other.write_text('> Approved source: https://linear.app/acme/issue/ENG-9\n\n'
                         'Related: https://linear.app/acme/issue/ENG-1\n')
        r={'session':'gaffer-acme-eng-9','instance':'acme','plan':str(other),
           'status':'running','transport':'exec','issue':'ENG-9'}
        c.s.write(self.state/'gaffers'/(r['session']+'.json'),r)
        with patch.object(c,'Linear') as linear:
            linear.return_value.approved.return_value=[self.issue]
            linear.return_value.call.return_value=self.issue
            self.assertEqual(c.intake('acme',self.cfg),[])
        sessions={x['session']:x for x in c.s.records()}
        self.assertEqual(sessions['gaffer-acme-eng-9']['issue'],'ENG-9')
        self.assertIn('gaffer-acme-ticket-eng-1',sessions)
        self.assertEqual(sessions['gaffer-acme-ticket-eng-1']['issue'],'ENG-1')

    def test_legacy_plan_declaring_the_source_is_adopted(self):
        legacy=self.root/'hand-written-plan.md'
        legacy.write_text('> Source RFC: https://linear.app/acme/issue/ENG-1\n'
                          '> Approval: created in Todo by a human.\n\nwork\n')
        r={'session':'gaffer-acme-legacy','instance':'acme','plan':str(legacy),
           'status':'running','transport':'exec'}
        c.s.write(self.state/'gaffers'/(r['session']+'.json'),r)
        with patch.object(c,'Linear') as linear:
            linear.return_value.approved.return_value=[self.issue]
            linear.return_value.call.return_value=self.issue
            self.assertEqual(c.intake('acme',self.cfg),[])
        records=c.s.records()
        self.assertEqual(len(records),1)
        self.assertEqual(records[0]['session'],'gaffer-acme-legacy')
        self.assertEqual(records[0]['issue'],'ENG-1')

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
        self.cfg['harness']='codex'
        r=self.record();p=c.event(r['session'],'go',{'kind':'steering'})
        with self.fake('import sys,json;sys.stdin.read();print(json.dumps({"type":"turn.started"}),flush=True);print(json.dumps({"type":"turn.completed"}),flush=True)'):
            c.run_turn(r['session'])
        self.assertEqual(c.read(p)['status'],'done')
        self.assertTrue(c.read(self.base/'turns'/(r['session']+'.json'))['acknowledged'])

    def test_claude_stream_json_acknowledges_and_completes(self):
        # system/init opens the turn and carries the session id; one result
        # closes it. Progress frames in between must not complete the turn.
        self.cfg['harness']='claude'
        r=self.record();p=c.event(r['session'],'go',{'kind':'steering'})
        script=('import sys,json;sys.stdin.read();'
                'print(json.dumps({"type":"system","subtype":"init","session_id":"sess-1"}),flush=True);'
                'print(json.dumps({"type":"assistant","message":{}}),flush=True);'
                'print(json.dumps({"type":"result","subtype":"success","is_error":False}),flush=True)')
        with self.fake(script):
            c.run_turn(r['session'])
        turn=c.read(self.base/'turns'/(r['session']+'.json'))
        self.assertEqual(c.read(p)['status'],'done')
        self.assertTrue(turn['acknowledged'])
        self.assertEqual(turn['thread_id'],'sess-1')
        self.assertEqual(turn['harness'],'claude')

    def test_claude_error_result_is_failure_and_retries(self):
        self.cfg['harness']='claude'
        r=self.record();p=c.event(r['session'],'go',{'kind':'steering'})
        script=('import sys,json;sys.stdin.read();'
                'print(json.dumps({"type":"system","subtype":"init","session_id":"sess-2"}),flush=True);'
                'print(json.dumps({"type":"result","subtype":"error_during_execution","is_error":True}),flush=True)')
        with self.fake(script):
            c.run_turn(r['session'])
        # #29: a failed turn blocks for owner recovery; it never silently retries.
        self.assertEqual(c.read(p)['status'],'blocked')
        self.assertEqual(c.read(p)['attempts'],1)

    def test_command_honors_configured_harness(self):
        for kind, exe in (('claude','claude'), ('codex','codex')):
            cfg=dict(self.cfg); cfg['harness']=kind; cfg['model']='m'; cfg['effort']='high'
            argv=c.command('gaffer','gaffer-acme-x',cfg,self.root)
            self.assertEqual(argv[0], str(c.ROOT/'scripts/factory-as.sh'))
            self.assertEqual(argv[3], exe)
            self.assertIn('m', argv)
        cfg=dict(self.cfg); cfg['harness']='nope'
        with self.assertRaises(ValueError):
            c.command('gaffer','gaffer-acme-x',cfg,self.root)

    def test_exit_zero_without_ack_blocks_without_timer_retry(self):
        r=self.record();p=c.event(r['session'],'go',{'kind':'steering'})
        with self.fake('import sys;sys.stdin.read()'):
            c.run_turn(r['session'])
        self.assertEqual(c.read(p)['status'],'blocked')
        self.assertEqual(c.read(p)['attempts'],1)
        with patch.object(c, 'execute') as again:
            c.run_turn(r['session']); again.assert_not_called()

    def test_abandoned_running_event_requires_explicit_recovery(self):
        r=self.record();p=c.event(r['session'],'go',{'kind':'steering'});e=c.read(p);e.update(status='running',attempts=1,run='lost-run');c.s.write(p,e)
        with patch.object(c,'execute') as execute:
            c.run_turn(r['session']); execute.assert_not_called()
        after=c.read(p)
        self.assertEqual(after['status'],'blocked')
        self.assertEqual(after['run'],'lost-run')
        self.assertEqual(after['attempts'],1)


    def test_failed_turn_does_not_retry_on_later_polls(self):
        r=self.record();p=c.event(r['session'],'go',{'kind':'steering'})
        with self.fake('import sys;sys.stdin.read();sys.exit(7)'):
            for _ in range(3):
                e=c.read(p);e['not_before']=0;c.s.write(p,e);c.run_turn(r['session'])
        self.assertEqual(c.read(p)['status'],'blocked')
        self.assertEqual(c.read(p)['attempts'],1)

    def test_hold_prevents_processing(self):
        r=self.record();p=c.event(r['session'],'go',{'kind':'steering'})
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

    def test_started_issue_comments_wake_and_backlog_pauses_assignment(self):
        r=self.record()
        issue=dict(self.issue,status='In Progress',statusType='started')
        with patch.object(c,'Linear') as linear:
            linear.return_value.approved.return_value=[]
            linear.return_value.call.side_effect=[issue,{'comments':[]}]
            c.intake('acme',self.cfg)
            queue=self.base/'queues'/r['session']
            self.assertEqual(len(list(queue.glob('*.json'))),0)
            linear.return_value.call.side_effect=[issue,{'comments':[{'id':'new','body':'steer'}]}]
            c.intake('acme',self.cfg)
            self.assertEqual(len(list(queue.glob('*.json'))),1)
            issue.update(status='Backlog',statusType='backlog')
            linear.return_value.call.side_effect=[issue,{'comments':[]}]
            c.intake('acme',self.cfg)
            self.assertTrue(c.read(self.state/'gaffers'/(r['session']+'.json'))['source_paused'])
        with patch.object(c,'execute') as execute:
            c.run_turn(r['session']);execute.assert_not_called()

    def test_source_bookkeeping_is_quiet_human_edits_and_reverts_are_events(self):
        r=self.record(); issue=dict(self.issue,status='In Progress',statusType='started')
        with patch.object(c,'Linear') as linear:
            linear.return_value.approved.return_value=[]
            def observe(comments):
                linear.return_value.call.side_effect=[issue,{'comments':comments}]
                c.intake('acme',self.cfg)
            observe([])
            issue['updatedAt']='later';observe([])
            observe([{'id':'bot','body':'report','user':{'id':'bot'}}])
            self.assertEqual(list((self.base/'queues'/r['session']).glob('*.json')),[])
            human={'id':'human-comment','body':'change direction','user':{'id':'human'}}
            observe([human]);observe([human])
            human['body']='changed again';observe([human])
            human['body']='change direction';observe([human])
            events=[c.read(p) for p in (self.base/'queues'/r['session']).glob('*.json')]
            self.assertEqual(len(events),3)
            self.assertTrue(all(e['payload']['kind']=='linear-steering' for e in events))

    def test_source_pause_is_visible_while_assignment_lock_is_held(self):
        r=self.record(); issue=dict(self.issue,status='Backlog',statusType='backlog')
        with patch.object(c,'Linear') as linear,c.gate(self.base/'locks'/(r['session']+'.lock')):
            linear.return_value.approved.return_value=[]
            linear.return_value.call.side_effect=[issue,{'comments':[]}]
            c.intake('acme',self.cfg)
            self.assertTrue(c.source_paused(r))
        with patch.object(c,'execute') as execute:
            c.run_turn(r['session']);execute.assert_not_called()

    def test_each_call_records_exactly_one_event_and_retains_each_receipt(self):
        r=self.record()
        for key in ('one','two'):c.event(r['session'],key,{'kind':'steering'})
        with self.fake('import sys;sys.stdin.read();print(\'{"type":"turn.started"}\');print(\'{"type":"turn.completed"}\')'):
            c.run_turn(r['session'])
            self.assertEqual(sum(e['status']=='done' for _,e in c.pending_events(r['session'])),1)
            c.run_turn(r['session'])
        receipts=[c.read(p) for p in (self.base/'runs'/r['session']).glob('*/receipt.json')]
        self.assertEqual({r['event_key'] for r in receipts},{'one','two'})
        self.assertEqual(len(c.read(self.state/'gaffers'/(r['session']+'.json'))['model_turns']),2)

    def test_health_reports_orphan_queue_immediately(self):
        c.s.write(self.base/'health.json',{'polled_at':c.time.time(),'problems':[]})
        c.event('gaffer-acme-orphan','lost-owner',{'kind':'worker-failed'})
        import contextlib,io
        output=io.StringIO()
        with contextlib.redirect_stdout(output):self.assertEqual(c.health('acme'),1)
        self.assertIn('no assignment record',output.getvalue())
        self.assertIn('lost-owner',output.getvalue())

    def test_watchdog_does_not_signal_reused_unrelated_pid(self):
        c.s.write(self.base/'turns/x.json',{'session':'x','status':'running','pid':123,'started_at':0})
        with patch.object(c.s,'run') as run,patch.object(c,'active',return_value=True),patch.object(c.os,'killpg') as kill:
            run.return_value.stdout='/bin/unrelated'
            c.watchdog();kill.assert_not_called()

    def test_watchdog_accepts_codex_node_launcher_with_matching_birth(self):
        c.s.write(self.base/'turns/x.json',{'session':'x','status':'running','pid':123,'started_at':0,'process_born':'same'})
        from types import SimpleNamespace
        with patch.object(c.s,'run',side_effect=[SimpleNamespace(stdout='node /opt/bin/codex'),SimpleNamespace(stdout='same')]),patch.object(c,'active',return_value=True),patch.object(c.os,'getpgid',return_value=123),patch.object(c.os,'killpg') as kill:
            c.watchdog();kill.assert_called_once_with(123,c.signal.SIGTERM)

    def test_report_observation_is_deduplicated_and_does_not_gate_assignment(self):
        r=self.record();(self.base/'enabled').touch()
        report=self.state/'gaffers'/(r['session']+'.report.md');report.write_text('progress')
        with patch.object(c,'intake',return_value=[]),patch.object(c,'spawn') as spawn:
            c.poll();c.poll()
            queue=list((self.base/'queues/foreman').glob('*.json'))
            self.assertEqual(len(queue),1)
            self.assertNotIn(unittest.mock.call(r['session']),spawn.call_args_list)
            report.write_text('new progress');c.poll()
            self.assertEqual(len(list((self.base/'queues/foreman').glob('*.json'))),2)

    def test_attended_receipt_requires_approved_state_team_and_human(self):
        with patch.dict(os.environ,FACTORY_ROLE='foreman'),self.assertRaisesRegex(ValueError,'attended'):
            c.record_approval('acme','ENG-1','human',[])
        with patch.dict(os.environ,FACTORY_ROLE='reception'),patch.object(c,'Linear') as linear:
            issue=dict(self.issue,status='Todo',team='team')
            linear.return_value.call.return_value=issue
            c.record_approval('acme','ENG-1','human',['acme/app'])
            self.assertEqual(c.read(self.base/'approvals/ENG-1.json')['repos'],['acme/app'])
            issue['status']='Backlog'
            with self.assertRaisesRegex(ValueError,'approved state'):c.record_approval('acme','ENG-1','human',[])
            with self.assertRaisesRegex(ValueError,'configured human'):c.record_approval('acme','ENG-1','bot',[])

    def test_start_timeout_blocks_without_manual_input(self):
        r=self.record();p=c.event(r['session'],'go',{'kind':'steering'})
        with patch.dict(os.environ,FACTORY_START_TIMEOUT='1'),self.fake('import sys,time;sys.stdin.read();time.sleep(30)'):
            c.run_turn(r['session'])
        self.assertEqual(c.read(p)['status'],'blocked')
        self.assertEqual(c.read(self.base/'turns'/(r['session']+'.json'))['error'],'TimeoutError')

if __name__=='__main__':unittest.main()
