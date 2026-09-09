#!/usr/bin/env python3
"""Cross-repository exact-session comparison acceptance with synthetic usage."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
import tempfile

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--kit-bin', required=True)
p.add_argument('--prices', required=True)
a = p.parse_args()
script = Path(__file__).parent/'costs/compare_claude.py'
with tempfile.TemporaryDirectory() as tmp:
    root = Path(tmp)
    (root/'ids.json').write_text('[{"session_id":"fixture"}]')
    def message(key, ts, count):
        return {'type':'assistant','sessionId':'fixture','timestamp':ts,
                'message':{'id':key,'model':'claude-sonnet-5','usage':{'input_tokens':count}}}
    rows=[message('one','2026-09-07T00:00:01Z',10), message('one','2026-09-07T00:00:01Z',10),
          message('two','2026-09-07T00:00:03Z',20),
          {'type':'cost-state','sessionId':'fixture','totalCostUSD':.0001,'hasUnknownModelCost':False,
           'modelUsage':{'claude-sonnet-5':{'inputTokens':50}}},
          message('after-snapshot','2026-09-07T00:00:05Z',999)]
    child=root/'fixture/subagents'
    child.mkdir(parents=True)
    (child/'agent.jsonl').write_text(json.dumps(message('child','2026-09-07T00:00:02Z',20))+'\n')
    def run():
        (root/'fixture.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in rows))
        return subprocess.run([sys.executable,str(script),'--claude-root',str(root),'--session-records',str(root/'ids.json'),
                    '--prices',a.prices,'--hev',a.kit_bin,'--output',str(root/'result.json')],capture_output=True,text=True)
    result=run()
    assert result.returncode==0, result.stdout+result.stderr
    assert json.loads((root/'result.json').read_text())['passed']==1
    rows[3]['modelUsage']['auxiliary']={'inputTokens':1}
    result=run()
    assert result.returncode==1, result.stdout+result.stderr
    audit=json.loads((root/'result.json').read_text())['rows'][0]
    assert audit['relative_error']<.05 and not audit['passed'] and audit['usage_gaps']
print('PASS: exact cost snapshot boundary, duplicate request, exact subagent join, historical kit rate, missing auxiliary usage rejects coincidental dollar match')
