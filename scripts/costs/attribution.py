"""Exact attribution evidence extraction; this module never emits prompt text."""
import json
import base64
import re

ISSUE = re.compile(r'(?<![A-Za-z0-9-])[A-Z][A-Z0-9]*-[0-9]+(?![0-9])')
FIELD = re.compile(r'(?im)^\s*(?:[-*>]\s*)?(?:\*\*)?(?:Human RFC|Human issue|Issue|Issue URL|Linear issue|RFC|Source)\s*(?:\*\*)?\s*:\s*(?:\*\*)?\s*(.+)$')
PLAN = re.compile(r'(plans/(?:active|archive)/([A-Za-z0-9_-]+)\.md)')
BRIEF = re.compile(r'(/[^\s`"<>]+/\.factory/briefs/([^/\s]+)/[^\s`"<>]+\.md)')


def texts(value):
    if isinstance(value, str):
        try:
            parsed = json.loads(value)
        except (ValueError, TypeError):
            return [value]
        return texts(parsed) if parsed != value else [value]
    if isinstance(value, list):
        return [s for v in value for s in texts(v)]
    if isinstance(value, dict):
        if value.get('encoding')=='base64' and isinstance(value.get('content'),str):
            try:return [base64.b64decode(value['content']).decode()]
            except (ValueError,UnicodeError):return []
        return [s for k in ('text', 'output', 'content', 'message') if k in value for s in texts(value[k])]
    return []


def fields(text):
    return {key for line in FIELD.findall(text) for key in ISSUE.findall(line)}


class Evidence:
    def __init__(self):
        self.briefs = {}
        self.documents = set()
        self.plans = set()
        self.calls = set()
        self.issues = set()
        self.parent = None
        self.sources = set()
        self.session_names = set()

    def read(self, rec):
        if 'payload' not in rec:
            message = rec.get('message') or {}
            content = message.get('content', [])
            if isinstance(content, str): content = [{'type':'text','text':content}]
            for block in content:
                if not isinstance(block, dict): continue
                kind = block.get('type')
                if kind == 'text' and message.get('role') == 'user':
                    self.read({'payload':{'type':'message','role':'user','content':[block]}})
                elif kind == 'tool_use':
                    self.read({'payload':{'type':'function_call','call_id':block.get('id'),'arguments':block.get('input')}})
                elif kind == 'tool_result':
                    self.read({'payload':{'type':'function_call_output','call_id':block.get('tool_use_id'),'output':block.get('content')}})
            return
        p = rec.get('payload', {})
        if rec.get('type') == 'session_meta':
            source = p.get('source', {})
            if isinstance(source, dict):
                self.parent = source.get('subagent', {}).get('thread_spawn', {}).get('parent_thread_id')
        if p.get('role') == 'user' and p.get('type') == 'message':
            for text in texts(p.get('content')):
                # Brief references establish which later tool output is the
                # assignment, rather than treating every issue mentioned as it.
                for path, instance in BRIEF.findall(text):
                    self.briefs[path] = instance
                    self.documents.add(path)
                for path,plan in PLAN.findall(text):
                    self.documents.add(path)
                    self.plans.add(plan)
                found = fields(text)
                if found:
                    self.issues.update(found)
                    self.sources.add('explicit_user_assignment')
        if p.get('type') in ('function_call', 'custom_tool_call'):
            args = p.get('arguments') or p.get('input') or ''
            if not isinstance(args, str):
                args = json.dumps(args)
            if any(path in args for path in self.documents):
                self.calls.add(p.get('call_id') or p.get('id'))
        if p.get('type') in ('function_call_output', 'custom_tool_call_output') and p.get('call_id') in self.calls:
            for text in texts(p.get('output')):
                found = fields(text)
                if found:
                    self.issues.update(found)
                    self.sources.add('recorded_assignment_document')

    def result(self):
        out = {'_brief_paths': sorted(self.briefs)}
        if len(self.issues) == 1:
            out['issue'] = next(iter(self.issues))
        elif self.issues:
            out['issue_candidates'] = sorted(self.issues)
        if len(self.plans)==1: out['plan']=next(iter(self.plans))
        instances = set(self.briefs.values())
        if len(instances) == 1:
            out['instance'] = next(iter(instances))
            out['role'] = 'worker'
        if self.parent:
            out['parent_session_id'] = self.parent
        if self.sources:
            out['attribution_sources'] = sorted(self.sources)
        return out
