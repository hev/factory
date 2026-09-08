import importlib.util
import json
from pathlib import Path
import tempfile
import subprocess
import os
import sys
import datetime as dt
import unittest

spec = importlib.util.spec_from_file_location('ingest', Path(__file__).parents[1]/'costs/ingest.py')
ingest = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ingest)

class Ingestion(unittest.TestCase):
    def parse(self, records, harness='codex'):
        with tempfile.TemporaryDirectory() as d:
            p = Path(d)/'rollout.jsonl'
            p.write_text('\n'.join(json.dumps(r) for r in records))
            return ingest.parse(p, harness)

    def test_cumulative_duplicate_and_model_switch(self):
        meta = {'type':'session_meta','payload':{'id':'s','model':'model-a','cwd':'/test'}}
        def tokens(i,c,o,r):
            return {'type':'event_msg','payload':{'type':'token_count','info':{'total_token_usage':{'input_tokens':i,'cached_input_tokens':c,'output_tokens':o,'reasoning_output_tokens':r}}}}
        row = self.parse([meta,tokens(100,80,20,10),tokens(100,80,20,10),
                          {'type':'turn_context','payload':{'model':'model-b'}},tokens(150,100,40,15)])
        self.assertEqual(row['total_tokens'],190)
        self.assertEqual(row['input_tokens'],50)
        self.assertEqual(row['reasoning_tokens'],15)
        self.assertEqual(row['usage_by_model']['model-b']['input_tokens'],30)

    def test_claude_request_deduplication(self):
        r = {'type':'assistant','sessionId':'s','requestId':'r','message':{'model':'example','usage':{'input_tokens':10,'cache_read_input_tokens':20,'output_tokens':5}}}
        self.assertEqual(self.parse([r,r], 'claude_code')['total_tokens'],35)

    def test_ambiguous_last_usage_is_not_invented(self):
        row = self.parse([{'type':'event_msg','payload':{'type':'token_count','info':{'last_token_usage':{'input_tokens':1}}}}])
        self.assertIn('accounting_error',row)

    def test_shards_replay_without_duplicates_and_keep_session_start(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            (root/'claude').mkdir()
            (root/'codex').mkdir()
            now = dt.datetime.now(dt.timezone.utc)
            for i, tokens in enumerate((100, 20)):
                row = {'timestamp': (now+dt.timedelta(seconds=i)).isoformat(), 'type':'assistant',
                       'sessionId':'s', 'requestId':str(i), 'message':{'model':'fixture','usage':{'input_tokens':tokens}}}
                (root/'claude'/f'{i}.jsonl').write_text(json.dumps(row)+'\n')
            script = Path(__file__).parents[1]/'costs/ingest.py'
            args = [sys.executable,str(script),'--codex-root',str(root/'codex'),'--claude-root',str(root/'claude'),
                    '--factory-root',str(root/'factory'),'--output',str(root/'sessions.jsonl')]
            subprocess.run(args,check=True,capture_output=True)
            first = (root/'sessions.jsonl').read_text()
            subprocess.run(args,check=True,capture_output=True)
            self.assertEqual(first,(root/'sessions.jsonl').read_text())
            row = json.loads(first)
            self.assertEqual(row['total_tokens'],120)
            self.assertEqual(row['start'],int(now.timestamp()*1000))

    def test_unfinished_live_tail_is_explicit(self):
        with tempfile.TemporaryDirectory() as d:
            p = Path(d)/'rollout.jsonl'
            p.write_text('{"type":"session_meta","payload":{"id":"s"}}\n{"partial":')
            self.assertEqual(ingest.parse(p,'codex')['accounting_error'],'unfinished transcript record')

    def test_exact_ledger_join_and_harvest(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            (root/'evals').mkdir()
            (root/'harvest'/'example').mkdir(parents=True)
            (root/'evals/evals.jsonl').write_text(json.dumps({'session':'uuid','session_name':'worker-example-job','role':'worker','instance':'example'})+'\n')
            ledger = {'session':'worker-example-job','issue':'EX-1','pr':7}
            (root/'harvest/example/job.log').write_text('# ledger:\n# '+json.dumps(ledger)+'\nprivate pane\n')
            rows = ingest.metadata(root)
            self.assertEqual(rows['uuid']['issue'],'EX-1')
            self.assertEqual(rows['uuid']['pr'],7)
            self.assertNotIn('private',str(rows))

class BeatAccounting(unittest.TestCase):
    def test_json_numbers_and_pending_null(self):
        with tempfile.TemporaryDirectory() as d:
            script = Path(__file__).parents[1]/'factory-beat.sh'
            subprocess.run(['bash',str(script),'example','api_usd=1e-07','sub_usd=null'],check=True,env={**os.environ,'FACTORY_BEAT_DIR':d})
            row = json.loads((Path(d)/'example.jsonl').read_text())
            self.assertEqual(row['api_usd'],1e-7)
            self.assertIsNone(row['sub_usd'])

if __name__ == '__main__': unittest.main()
