#!/usr/bin/env python3
"""Linear MCP stdio bridge; resolve the configured identity afresh per request.

Streamable HTTP framing: https://modelcontextprotocol.io/specification/2025-06-18/basic/transports
No listening socket, no token in argv or protocol output. No retries of writes.
"""
import fcntl
import json
import os
from pathlib import Path
import subprocess
import sys
import tomllib
import urllib.request

ROOT = Path(__file__).resolve().parent.parent


def token(instance, server):
    # The same secret seam and stored OAuth grant as factory-iterate.sh. A
    # persistent session must not freeze a 24-hour access token in its env.
    lock_path = Path.home() / '.factory/mcp-refresh.lock'
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        result = subprocess.run(['bash', '-c',
            'source "$1/scripts/lib/secrets.sh"; factory_secret LINEAR_MCP_TOKEN "$2"',
            'factory-mcp', str(ROOT), instance], capture_output=True, text=True, timeout=30)
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout.strip()
        subprocess.run([sys.executable, str(ROOT / 'scripts/mcp-refresh.py'), '--server', server],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=45, check=True)
        credentials = json.loads((Path.home() / '.claude/.credentials.json').read_text())
        for entry in credentials.get('mcpOAuth', {}).values():
            if entry.get('serverName') == server and entry.get('accessToken'):
                return entry['accessToken']
    raise ValueError('no credential for configured factory MCP server')


class Bridge:
    def __init__(self, instance):
        cfg = tomllib.loads((ROOT / 'factories' / (instance + '.toml')).read_text())
        if not cfg.get('linear_team'):
            raise ValueError('this factory does not use Linear')
        self.instance = instance
        self.server = cfg.get('linear_mcp_server', 'linear')
        self.session = None
        self.version = None

    def send(self, message):
        headers = {'Authorization': 'Bearer ' + token(self.instance, self.server),
                   'Content-Type': 'application/json', 'Accept': 'application/json, text/event-stream'}
        if self.session:
            headers['Mcp-Session-Id'] = self.session
        if self.version:
            headers['MCP-Protocol-Version'] = self.version
        request = urllib.request.Request('https://mcp.linear.app/mcp',
            data=json.dumps(message).encode(), headers=headers)
        with urllib.request.urlopen(request, timeout=90) as response:
            self.session = response.headers.get('Mcp-Session-Id', self.session)
            if response.status == 202:
                return
            if 'text/event-stream' in response.headers.get('Content-Type', ''):
                data = []
                for line in response:
                    line = line.decode().rstrip('\r\n')
                    if line.startswith('data:'):
                        data.append(line[5:].lstrip(' '))
                    elif not line and data:
                        answer = json.loads('\n'.join(data))
                        data = []
                        self.emit(answer)
                        if 'id' in answer and answer['id'] == message.get('id'):
                            return
                raise ValueError('MCP stream ended without a response')
            self.emit(json.load(response))

    def emit(self, answer):
        version = answer.get('result', {}).get('protocolVersion')
        if version:
            self.version = version
        print(json.dumps(answer), flush=True)


def main():
    instance = sys.argv[1]
    if not instance or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-' for c in instance):
        raise ValueError('invalid instance')
    bridge = Bridge(instance)
    for line in sys.stdin:
        message = json.loads(line)
        try:
            bridge.send(message)
        except Exception as exc:
            # Never print the request, HTTP body, credential or subprocess output.
            print('factory-mcp: request failed (' + type(exc).__name__ + ')', file=sys.stderr)
            if 'id' in message:
                print(json.dumps({'jsonrpc':'2.0', 'id':message['id'],
                    'error':{'code':-32603, 'message':'Factory MCP request failed; check configured bot authentication and connectivity. Do not retry a write without checking its result.'}}), flush=True)


if __name__ == '__main__':
    main()
