#!/usr/bin/env python3
"""Read an existing GUI-domain Claude grant; persist measured usage, never credentials."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
import time
import urllib.request
from ingest import atomic_write


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--plan',required=True)
    p.add_argument('--output',type=Path,required=True)
    a=p.parse_args()
    credential=subprocess.run(['security','find-generic-password','-s','Claude Code-credentials','-w'],capture_output=True,text=True,timeout=15)
    if credential.returncode: raise RuntimeError('existing Claude keychain grant unavailable; no grant was created')
    token=json.loads(credential.stdout).get('claudeAiOauth',{}).get('accessToken')
    if not token: raise RuntimeError('existing Claude credential has no access token')
    request=urllib.request.Request('https://api.anthropic.com/api/oauth/usage',headers={'Authorization':'Bearer '+token,'anthropic-beta':'oauth-2025-04-20'})
    with urllib.request.urlopen(request,timeout=20) as response: raw=json.load(response)
    limits=[{k:v for k,v in limit.items() if k in ('kind','percent','resets_at')} for limit in raw.get('limits',[]) if limit.get('kind')=='weekly_all']
    if not limits and raw.get('seven_day'):
        limits=[{'kind':'weekly_all','percent':raw['seven_day'].get('utilization'),'resets_at':raw['seven_day'].get('resets_at')}]
    if not limits: raise RuntimeError('usage response contains no measured weekly limit')
    out={'plan':a.plan,'observed_at':int(time.time()*1000),'limits':limits}
    a.output.parent.mkdir(parents=True,exist_ok=True)
    atomic_write(a.output,json.dumps(out)+'\n')
    print('recorded measured Claude weekly usage')


if __name__=='__main__':
    try:main()
    except Exception as e:
        # Exceptions from credential/network tooling never print request headers
        # or raw response bodies. Missing access remains an explicit failure.
        print('Claude usage read failed: '+type(e).__name__,file=sys.stderr)
        sys.exit(1)
