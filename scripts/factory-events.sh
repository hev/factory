#!/bin/bash
# factory-events.sh — what the factory has said, and to whom.
#
# Usage:
#   scripts/factory-events.sh <instance>              new events; advances the cursor
#   scripts/factory-events.sh <instance> --peek       same, without advancing
#   scripts/factory-events.sh <instance> --tail N     last N events, cursor untouched
#   scripts/factory-events.sh <instance> --count      how many are unread (a number)
#   scripts/factory-events.sh <instance> --full       do not truncate multi-line text
#   scripts/factory-events.sh <instance> --reader X   read as X (default: reception)
#
# **Every reader gets its own cursor.** The front desk and the gaffer both read
# this spool for different reasons and on different clocks; one shared position
# would mean whichever got there first blinded the other. The gaffer passes
# `--reader gaffer` and keeps its own place.
#
# The spool (~/.factory/events/<instance>.jsonl) has two kinds of line in it and
# the difference is the whole point:
#
#   →slack   the gaffer already posted this outward. The operator has seen it.
#   (blank)  a worker said it to the desk. Nobody outside has heard it.
#
# That second column is what stops the front desk repeating back something
# Slack carried an hour ago, and it is what tells it when a worker blocked two
# minutes after a beat closed — invisible until the next beat, under every
# other way of looking.
#
# **Reported is not true.** Every line here is testimony from an agent about
# its own state. A worker saying "pull request opened" is a claim; `gh` and the
# child ledger are the facts, and scripts/factory-health.sh is still the answer
# to whether anything is wedged. Quote the spool as what somebody said.

set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EVENTS_DIR="${FACTORY_EVENTS_DIR:-$HOME/.factory/events}"

INSTANCE=""; MODE="new"; TAIL_N=20; FULL=0; READER="reception"
while [[ $# -gt 0 ]]; do
    case "$1" in
        --peek)   MODE="peek"; shift ;;
        --count)  MODE="count"; shift ;;
        --full)   FULL=1; shift ;;
        --tail)   MODE="tail"; TAIL_N="${2:-20}"; shift 2 ;;
        --reader) READER="${2:-reception}"; shift 2 ;;
        -h|--help) sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        -*) echo "factory-events: unknown flag: $1" >&2; exit 2 ;;
        *)  INSTANCE="$1"; shift ;;
    esac
done
[[ -n "$INSTANCE" ]] || { echo "usage: $0 <instance> [--peek|--tail N|--count|--full] [--reader X]" >&2; exit 2; }
READER="$(printf '%s' "$READER" | tr -cs 'a-zA-Z0-9-' '-' | sed 's/-$//')"
[[ -n "$READER" ]] || READER="reception"

spool="$EVENTS_DIR/$INSTANCE.jsonl"
cursor="$EVENTS_DIR/$INSTANCE.cursor.$READER"

# No spool is the normal state of a quiet factory, not a fault.
if [[ ! -f "$spool" ]]; then
    [[ "$MODE" == "count" ]] && { echo 0; exit 0; }
    echo "no events yet for $INSTANCE"
    exit 0
fi

total="$(wc -l < "$spool" | tr -d ' ')"
seen=0
[[ -f "$cursor" ]] && seen="$(tr -cd '0-9' < "$cursor")"
seen="${seen:-0}"
# A truncated or rotated spool must not leave the cursor past the end.
[[ "$seen" -gt "$total" ]] && seen=0

if [[ "$MODE" == "count" ]]; then
    echo $(( total - seen ))
    exit 0
fi

if [[ "$MODE" == "tail" ]]; then
    lines="$(tail -n "$TAIL_N" "$spool")"
else
    if [[ "$seen" -ge "$total" ]]; then
        echo "nothing new for $INSTANCE (${total} events, all read by $READER)"
        exit 0
    fi
    lines="$(tail -n +$(( seen + 1 )) "$spool")"
fi

render() {
    if command -v jq >/dev/null 2>&1; then
        jq -r --argjson full "$FULL" '
            def head: if $full == 1 then .text
                      else (.text | split("\n")) as $l
                           | if ($l|length) > 1 then ($l[0] + " (+" + (($l|length)-1|tostring) + " more lines)")
                             else $l[0] end
                      end;
            [ (.ts | sub("T"; " ") | sub("Z"; "")),
              .from,
              .kind,
              (if .outward then "→slack" else "" end),
              head
            ] | @tsv' 2>/dev/null
    else
        cat
    fi
}

printf '%s\n' "$lines" | render

if [[ "$MODE" == "new" ]]; then
    printf '%s\n' "$total" > "$cursor"
fi
