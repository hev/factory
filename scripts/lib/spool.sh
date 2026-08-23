#!/bin/bash
# spool.sh — the one place a line gets written to the event spool.
#
# factory_spool_append <instance> <from> <kind> <outward:true|false> <text>
#
# Appends one JSON line to ~/.factory/events/<instance>.jsonl. Two callers:
# factory-say.sh (a worker talking to the desk, outward=false) and notify.sh
# (something that went to Slack, outward=true). That flag is the field the
# front desk reads to know what the operator has already seen.
#
# Never fails its caller. A lost line is not worth a failed beat or a dead
# worker.

factory_spool_append() {  # instance from kind outward text
    local instance="$1" from="$2" kind="$3" outward="$4" text="$5"
    local dir="${FACTORY_EVENTS_DIR:-$HOME/.factory/events}"
    local spool="$dir/$instance.jsonl"
    local ts line

    mkdir -p "$dir" 2>/dev/null || return 0
    ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    if command -v jq >/dev/null 2>&1; then
        line="$(jq -cn --arg ts "$ts" --arg instance "$instance" --arg from "$from" \
            --arg kind "$kind" --argjson outward "$outward" --arg text "$text" \
            '{ts:$ts, instance:$instance, from:$from, kind:$kind, outward:$outward, text:$text}')" || return 0
    else
        local esc
        esc() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g' | awk '{printf "%s\\n", $0}' | sed 's/\\n$//'; }
        line="$(printf '{"ts":"%s","instance":"%s","from":"%s","kind":"%s","outward":%s,"text":"%s"}' \
            "$ts" "$(esc "$instance")" "$(esc "$from")" "$kind" "$outward" "$(esc "$text")")"
    fi

    # One printf of one line: jsonl keeps a multi-line block on a single line,
    # and an O_APPEND write that small lands whole even with several workers
    # talking at once. No lock — macOS has no flock, and a lock nobody can take
    # is worse than the guarantee the kernel already gives.
    printf '%s\n' "$line" >> "$spool" 2>/dev/null || return 0
}
