"""Conservative harness outcomes and durable admission; no process launcher."""
from __future__ import annotations

from contextlib import contextmanager
from dataclasses import asdict, dataclass
from datetime import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import tempfile


@dataclass(frozen=True)
class Classification:
    kind: str
    reset: str | None = None
    safe_to_fallback: bool = False
    credits: dict | None = None

    def __post_init__(self):
        if self.kind not in ('ok', 'usage_limit', 'auth', 'other'):
            raise ValueError('invalid classification')
        if self.safe_to_fallback and self.kind != 'usage_limit':
            raise ValueError('only a proven refusal authorizes fallback')


def classify(harness, stream, returncode):
    """Read a complete, single-attempt JSONL stream, never arbitrary pane text."""
    if harness == 'claude':
        try:
            rows = [json.loads(line) for line in stream.splitlines() if line.strip()]
            if any(not isinstance(row, dict) for row in rows):
                return Classification('other')
        except ValueError:
            return Classification('other')
        if any(row.get('type') == 'assistant' and row.get('isApiErrorMessage') is True
               and row.get('error') == 'authentication_failed' for row in rows):
            return Classification('auth')
        return Classification('other')
    if harness != 'codex':
        return Classification('other')
    started = completed = activity = malformed = failed = False
    limit = uncertain = False
    reset = credits = None
    for line in stream.splitlines():
        if not line.strip():
            continue
        try:
            msg = json.loads(line)
            if not isinstance(msg, dict):
                raise ValueError()
            v = msg.get('payload', {}) if msg.get('type') in ('event_msg', 'response_item') else msg
            if not isinstance(v, dict):
                raise ValueError()
        except (ValueError, TypeError):
            malformed = True
            continue
        kind = v.get('type')
        uncertain |= msg.get('type') not in (
            'session_meta', 'turn_context', 'event_msg', 'response_item',
            'thread.started', 'turn.started', 'turn.completed',
            'item.started', 'item.completed', 'error', 'turn.failed')
        started |= kind == 'turn.started'
        completed |= kind == 'turn.completed'
        activity |= kind in ('item.started', 'item.completed', 'agent_message',
                             'agent_reasoning', 'exec_command_begin')
        activity |= msg.get('type') == 'response_item' and v.get('role') not in ('user', 'system', 'developer')
        uncertain |= msg.get('type') == 'event_msg' and kind not in (
            'task_started', 'user_message', 'token_count', 'task_complete')
        if kind == 'token_count':
            rates = v.get('rate_limits')
            if isinstance(rates, dict) and isinstance(rates.get('credits'), dict):
                credits = rates['credits']
        error = v.get('error')
        if kind == 'error' or kind == 'turn.failed' or error:
            failed = True
        if kind == 'task_complete' and isinstance(error, dict):
            code = error.get('codex_error_info')
            limit |= code in ('usage_limit', 'usage_limit_exceeded')
            # Auth has no locally recorded adapter yet: unknown codes remain other.
            if code in ('usage_limit', 'usage_limit_exceeded'):
                message = error.get('message', '')
                if not isinstance(message, str):
                    malformed = True
                    continue
                m = re.search(r'try again at ([A-Z][a-z]{2}) (\d+)(?:st|nd|rd|th), (\d{4}) (\d+:\d{2} [AP]M)', message)
                if m:
                    try:
                        reset = datetime.strptime(' '.join(m.groups()), '%b %d %Y %I:%M %p').isoformat(timespec='minutes')
                    except ValueError:
                        reset = None
            activity |= bool(v.get('last_agent_message'))
    if malformed:
        return Classification('other', credits=credits)
    if limit:
        return Classification('usage_limit', reset, not activity and not completed and not uncertain, credits)
    return Classification('ok' if started and completed and not failed and returncode == 0 else 'other', credits=credits)


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()


def reset_epoch(reset, timezone=None):
    if reset is None:
        return None
    dt = datetime.fromisoformat(reset)
    if dt.tzinfo is None:
        if timezone is None:
            return None
        dt = dt.replace(tzinfo=timezone)
    return dt.timestamp()


class Store:
    def __init__(self, holds, host):
        if not host.strip():
            raise ValueError('host is required')
        self.directory = Path(holds) / 'harness' / digest(host.lower())
        self.directory.mkdir(parents=True, exist_ok=True)
        self.path = self.directory / 'state.json'

    @contextmanager
    def transaction(self):
        with (self.directory / 'lock').open('a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            state = json.loads(self.path.read_text()) if self.path.exists() else {
                'version': 1, 'breakers': {}, 'observations': {}, 'attempts': {}, 'reports': {}}
            if state.get('version') != 1 or any(not isinstance(state.get(k), dict) for k in
                    ('breakers', 'observations', 'attempts', 'reports')):
                raise ValueError('invalid harness state')
            yield state
            fd, temp = tempfile.mkstemp(dir=self.directory)
            try:
                with os.fdopen(fd, 'w') as out:
                    json.dump(state, out, sort_keys=True)
                    out.flush()
                    os.fsync(out.fileno())
                os.replace(temp, self.path)
                directory_fd = os.open(self.directory, os.O_RDONLY)
                try:
                    os.fsync(directory_fd)
                finally:
                    os.close(directory_fd)
            finally:
                if os.path.exists(temp):
                    os.unlink(temp)

    def admitted(self, harness, now):
        with self.transaction() as s:
            b = s['breakers'].get(harness)
            return b is None or (b['reset_epoch'] is not None and now >= b['reset_epoch'])

    def observe(self, harness, observation, result, now, timezone=None):
        if result.kind != 'usage_limit':
            return
        value = dict(harness=harness, result=asdict(result))
        with self.transaction() as s:
            if observation in s['observations']:
                if s['observations'][observation] != value:
                    raise ValueError('observation identity reused')
                return
            s['observations'][observation] = value
            epoch = reset_epoch(result.reset, timezone)
            old = s['breakers'].get(harness)
            if old and (old['reset_epoch'] is None or
                        (epoch is not None and old['reset_epoch'] >= epoch)):
                return
            b = dict(reset=result.reset, reset_epoch=epoch, observed_at=now)
            s['breakers'][harness] = b
            report_id = digest([harness, b])
            s['reports'][report_id] = dict(id=report_id, harness=harness, **b)

    def pending_reports(self):
        with self.transaction() as s:
            return list(s['reports'].values())

    def acknowledge_report(self, report_id):
        with self.transaction() as s:
            s['reports'].pop(report_id, None)


def run_attempt(store, attempt_id, identity, brief, primary, config, launch, now, timezone=None):
    """launch(harness, model, identity, brief) -> Classification.

    Adapter must preserve external locks/gates and prove complete transport output.
    A crash or exception leaves a reservation requiring owner reconciliation.
    """
    fallback = config.get('harness_fallback', 'claude')
    models = config.get('harness_models', {})
    if primary not in ('codex', 'claude') or fallback not in ('', 'codex', 'claude'):
        raise ValueError('unsupported harness')
    if not isinstance(models, dict) or any(k not in ('codex', 'claude') or
            not isinstance(v, str) for k, v in models.items()):
        raise ValueError('invalid harness_models')
    if any(not identity.get(k) for k in ('owner', 'task', 'event')):
        raise ValueError('owner/task/event required')
    fingerprint = digest([identity, brief, primary, config])
    with store.transaction() as s:
        if attempt_id in s['attempts']:
            saved = s['attempts'][attempt_id]
            if saved['fingerprint'] != fingerprint:
                raise ValueError('attempt identity reused with changed input')
            return saved
        receipt = dict(fingerprint=fingerprint, identity=dict(identity), brief_sha256=digest(brief),
                       status='blocked', reason='reserved; owner reconciliation required', harnesses=[])
        s['attempts'][attempt_id] = receipt
    for harness in dict.fromkeys([primary, fallback]):
        if not harness:
            continue
        admitted = store.admitted(harness, now)
        entry = dict(harness=harness, model=models.get(harness), status='reserved' if admitted else 'held')
        receipt['harnesses'].append(entry)
        with store.transaction() as s:
            s['attempts'][attempt_id] = receipt
        if not admitted:
            receipt['reason'] = 'harness held'
            continue
        result = launch(harness, models.get(harness), dict(identity), brief)
        entry.update(status='finished', classification=asdict(result))
        store.observe(harness, attempt_id + ':' + harness, result, now, timezone)
        receipt['reason'] = result.kind
        if result.kind == 'ok':
            receipt['status'] = 'ok'
            break
        if result.kind != 'usage_limit' or not result.safe_to_fallback:
            break
    with store.transaction() as s:
        s['attempts'][attempt_id] = receipt
    return receipt
