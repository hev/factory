#!/bin/bash
# gaffer-msg.sh — the channel to a gaffer.
#
# Usage: gaffer-msg.sh <instance> <steer|interrupt> <message> [context-url]
#
# The sender is reception unless FACTORY_MSG_FROM says otherwise. The picker
# sets it to `operator` when somebody watching the floor sends a worker's
# trouble down this rail, because a gaffer weighs a line from the person who
# owns the factory differently from one the desk is relaying, and it can only
# do that if the message says which it is.
#
# Writes one JSON message file atomically into ~/.factory/inbox/<instance>/.
# The gaffer drains the inbox as step 0 of every beat (steer: <= 1 beat).
# For priority "interrupt" — only when relaying an explicit operator order whose
# value expires before the next beat — the script additionally does whatever
# the instance's runtime allows to cut the wait short. Content always lives in
# the file, never in the mechanism:
#
#   resident   Escape plus the single sanctioned INTERRUPT line into the
#              gaffer's tmux session (gaffer-<instance>). Latency: seconds.
#   one-shot   There is no pane. Halt the in-flight iteration if there is one,
#              and drop a wake flag so the next scheduled fire ignores its
#              pacing hint. Latency: one launchd fire (interval_base, 300s by
#              default) rather than whatever the hint asked for.
#
# Neither path starts anything. Interrupting is the whole authority here.

set -euo pipefail

INSTANCE="${1:?usage: gaffer-msg.sh <instance> <steer|interrupt> <message> [context-url]}"
PRIORITY="${2:?priority: steer or interrupt}"
MSG="${3:?message required}"
CONTEXT="${4:-}"

case "$PRIORITY" in
    steer|interrupt) ;;
    *) echo "priority must be steer or interrupt" >&2; exit 1 ;;
esac

# Constrained to the same shape as an events reader name, so a caller cannot
# put arbitrary text — or a quote — into the `from` field of the JSON below.
FROM="$(printf '%s' "${FACTORY_MSG_FROM:-reception}" | tr -cs 'a-zA-Z0-9-' '-' | sed 's/-$//')"
[[ -n "$FROM" ]] || FROM="reception"

INBOX="${FACTORY_INBOX_DIR:-$HOME/.factory/inbox}/$INSTANCE"
mkdir -p "$INBOX/done"

slug="$(echo "$MSG" | tr -cs 'a-zA-Z0-9' '-' | cut -c1-40 | sed 's/^-//;s/-$//' | tr 'A-Z' 'a-z')"
file="$INBOX/$(date +%s)-${slug:-msg}.json"

# A JSON string cannot hold a raw newline, tab or carriage return. Messages
# used to be one line and this only escaped quotes and backslashes, which was
# enough right up until something sent a paragraph — the picker sends the
# operator's sentence with the worker's context under it — and wrote a file
# nothing downstream could parse. The newline is encoded rather than passed
# through; tabs become spaces, which is all a message needs of them.
esc() {
    printf '%s' "$1" \
        | tr -d '\r' | tr '\t' ' ' \
        | sed 's/\\/\\\\/g; s/"/\\"/g' \
        | awk 'BEGIN{ORS=""} NR>1{printf "\\n"} {printf "%s", $0}'
}

tmp="$file.tmp"
{
    printf '{"ts":"%s","from":"%s","priority":"%s","relaying_operator":%s,"msg":"%s"' \
        "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$FROM" "$PRIORITY" \
        "$([ "$PRIORITY" = interrupt ] && echo true || echo "${RELAYING_OPERATOR:-false}")" \
        "$(esc "$MSG")"
    [ -n "$CONTEXT" ] && printf ',"context":"%s"' "$(esc "$CONTEXT")"
    printf '}\n'
} > "$tmp"
mv "$tmp" "$file"
echo "delivered: $file"

[ "$PRIORITY" = interrupt ] || exit 0

# ── P0: cut the wait short by whatever the runtime permits ────

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
CONFIG="$ROOT_DIR/factories/$INSTANCE.toml"

runtime=resident
if [ -f "$CONFIG" ]; then
    runtime="$(awk -F= '
        $1 ~ /^[[:space:]]*runtime[[:space:]]*$/ {
            v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*#.*/,"",v)
            sub(/[[:space:]]*$/,"",v); gsub(/^"|"$/,"",v); print v; exit
        }' "$CONFIG")"
    runtime="${runtime:-resident}"
fi

if [ "$runtime" = "one-shot" ]; then
    state="$HOME/.factory/iterations/$INSTANCE"
    mkdir -p "$state"

    # The wake flag is read before the pacing gate, so the next fire runs even
    # if the last iteration asked for a long idle interval.
    : > "$state/wake"
    echo "woke: $INSTANCE (next fire ignores its pacing hint)"

    # If an iteration is in flight it is working from a world that predates
    # this message. Halt it: the wrapper exits without stamping the heartbeat,
    # and the next fire re-reads everything with the inbox drained at step 0.
    pid_file="$state/lock/pid"
    if [ -f "$pid_file" ]; then
        pid="$(cat "$pid_file" 2>/dev/null)"
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            pkill -TERM -P "$pid" 2>/dev/null || true
            kill -TERM "$pid" 2>/dev/null || true
            echo "halted: in-flight iteration (pid $pid)"
        fi
    fi
    exit 0
fi

session="factory-$INSTANCE"
if tmux has-session -t "$session" 2>/dev/null; then
    tmux send-keys -t "$session" Escape
    sleep 1
    tmux send-keys -t "$session" "INTERRUPT — read ~/.factory/inbox/$INSTANCE/ now" Enter
    echo "interrupted: $session"
else
    echo "warn: no tmux session $session — message waits in inbox for re-kick" >&2
fi
