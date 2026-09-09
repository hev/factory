import json
from pathlib import Path
import sys
import unittest
sys.path.insert(0,str(Path(__file__).parents[1]/'costs'))
from eval_rows import adapt

class EvalRows(unittest.TestCase):
    def test_structured_findings_preserve_every_field_and_identity(self):
        finding={'dimension':'evidence','artifact':'file','turns':[1,2],'fixable_by':'worker','suggestion':'Check "quotes"\nand newlines'}
        row={'session':'fixture','ts':'2026-09-07T00:00:00Z','marks':{'evidence':3},'findings':[finding,'legacy string'],'session_cost_usd':None}
        result=adapt(row)
        self.assertEqual(json.loads(result['findings'][0]),finding)
        self.assertEqual(result['findings'][1],'legacy string')
        self.assertEqual(result['session'],row['session'])
        self.assertEqual(result['ts'],row['ts'])
        self.assertNotIn('session_cost_usd',result)
        self.assertEqual(adapt(result),result)
        row['findings']=[dict(reversed(list(finding.items())))]
        self.assertEqual(adapt(row)['findings'][0],result['findings'][0])

    def test_invalid_rows_fail(self):
        for row in ({}, {'session':'s','ts':'t','marks':{},'findings':[42]}):
            with self.assertRaises(ValueError): adapt(row)
