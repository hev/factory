import sys
from pathlib import Path
import unittest
sys.path.insert(0, str(Path(__file__).parents[1]/'costs'))
from compare_claude import compare

class Comparison(unittest.TestCase):
    def test_exact_usage_required_even_within_five_percent(self):
        row={'session_id':'fixture','usage_by_model':{'model':{'input_tokens':100}},'api_usd':1}
        state={'modelUsage':{'model':{'inputTokens':100,'thinkingTokens':20}},'totalCostUSD':1,'hasUnknownModelCost':False}
        self.assertTrue(compare(row,state)['passed'])
        state['modelUsage']['auxiliary']={'inputTokens':1}
        self.assertFalse(compare(row,state)['passed'])
        del state['modelUsage']['auxiliary']
        row['api_usd']=None
        self.assertFalse(compare(row,state)['passed'])
        row['api_usd']=1.06
        self.assertFalse(compare(row,state)['passed'])
