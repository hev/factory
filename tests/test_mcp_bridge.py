import importlib.util
import io
import json
from pathlib import Path
import unittest
from unittest.mock import patch
from urllib.error import HTTPError

spec = importlib.util.spec_from_file_location('bridge', Path(__file__).resolve().parents[1] / 'scripts/factory-mcp.py')
bridge = importlib.util.module_from_spec(spec); spec.loader.exec_module(bridge)

class Response(io.BytesIO):
    status = 200
    def __init__(self, body, headers):
        super().__init__(body); self.headers = headers

class BridgeTest(unittest.TestCase):
    def setUp(self):
        self.bridge = bridge.Bridge.__new__(bridge.Bridge)
        self.bridge.instance = 'example'; self.bridge.server = 'linear'
        self.bridge.session = None; self.bridge.version = None

    def test_sse_response_carries_negotiated_session_and_version(self):
        response = Response(b'data: {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}\n\n',
                            {'Content-Type':'text/event-stream','Mcp-Session-Id':'fixture'})
        out = io.StringIO()
        with patch.object(bridge, 'token', return_value='private'), patch.object(bridge.urllib.request, 'urlopen', return_value=response), patch('sys.stdout', out):
            self.bridge.send({'jsonrpc':'2.0','id':1,'method':'initialize'})
        self.assertEqual(self.bridge.session,'fixture')
        self.assertEqual(self.bridge.version,'2025-06-18')
        self.assertEqual(json.loads(out.getvalue())['id'],1)
        self.assertNotIn('private',out.getvalue())

    def test_failed_write_is_not_replayed(self):
        with patch.object(bridge, 'token', return_value='private'), patch.object(bridge.urllib.request, 'urlopen', side_effect=HTTPError('url',503,'unavailable',{},None)) as http:
            with self.assertRaises(HTTPError):
                self.bridge.send({'jsonrpc':'2.0','id':2,'method':'tools/call','params':{'name':'update_issue'}})
        self.assertEqual(http.call_count,1)

if __name__ == '__main__': unittest.main()
