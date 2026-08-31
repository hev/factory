#!/bin/bash
# floor-watch.sh — deterministic speak-first duties for one factory.
#
# Called after the gaffer on every factory fire. It relays each new, non-outward
# blocked event once and reports a failing health check once per distinct
# result. Its entire memory is ~/.factory/reception/<instance>/spoken.

set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTANCE="${1:-}"
[[ -n "$INSTANCE" ]] || { echo "usage: $0 <instance>" >&2; exit 2; }
CONFIG="$ROOT_DIR/factories/$INSTANCE.toml"
[[ -f "$CONFIG" ]] || { echo "floor-watch: no config: $CONFIG" >&2; exit 1; }

read_config() {
    awk -F= -v key="$1" '$1 ~ "^[[:space:]]*" key "[[:space:]]*$" {
        v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*#.*/,"",v)
        sub(/[[:space:]]*$/,"",v); gsub(/^"|"$/,"",v); print v; exit
    }' "$CONFIG"
}

# A checkout copied to another host must remain quiet.
home_host="$(read_config home_host)"
host="$(hostname -s 2>/dev/null || hostname)"
host_fold="$(printf '%s' "$host" | tr '[:upper:]' '[:lower:]')"
home_fold="$(printf '%s' "$home_host" | tr '[:upper:]' '[:lower:]')"
[[ -z "$home_host" || "$host_fold" == "$home_fold" ]] || exit 0

state_dir="${FACTORY_RECEPTION_DIR:-$HOME/.factory/reception}/$INSTANCE"
spoken="$state_dir/spoken"
events="${FACTORY_EVENTS_DIR:-$HOME/.factory/events}/$INSTANCE.jsonl"
mkdir -p "$state_dir"

# Avoid duplicate posts when a manual fire overlaps launchd.
lock="$state_dir/.watch-lock"
mkdir "$lock" 2>/dev/null || exit 0
trap 'rmdir "$lock" 2>/dev/null || true' EXIT

seen=0
health_key=""
if [[ -f "$spoken" ]]; then
    seen="$(awk -F= '$1=="events" {print $2}' "$spoken" | tr -cd '0-9')"
    health_key="$(awk -F= '$1=="health" {sub(/^health=/,""); print}' "$spoken")"
fi
seen="${seen:-0}"
total=0
[[ -f "$events" ]] && total="$(wc -l < "$events" | tr -d ' ')"
[[ "$seen" -gt "$total" ]] && seen=0

if [[ -f "$events" && "$total" -gt "$seen" ]]; then
    tail -n "+$((seen + 1))" "$events" | while IFS= read -r line; do
        if command -v jq >/dev/null 2>&1 &&
           [[ "$(jq -r '.kind == "blocked" and (.outward | not)' <<<"$line" 2>/dev/null)" == true ]]; then
            from="$(jq -r '.from // "worker"' <<<"$line")"
            ask="$(jq -r '.text // "blocked"' <<<"$line")"
            printf '%s\n' "$from blocked: $ask — $INSTANCE floor watch" |
                "$ROOT_DIR/scripts/notify.sh" "$INSTANCE" floor-watch >/dev/null
        fi
    done
fi

health_out=""
if ! health_out="$("$ROOT_DIR/scripts/factory-health.sh" "$INSTANCE" 2>&1)"; then
    next_key="$(printf '%s' "$health_out" | shasum -a 256 | awk '{print $1}')"
    if [[ "$next_key" != "$health_key" ]]; then
        printf 'Factory health is failing:\n%s\n— %s floor watch\n' "$health_out" "$INSTANCE" |
            "$ROOT_DIR/scripts/notify.sh" "$INSTANCE" floor-watch >/dev/null
    fi
    health_key="$next_key"
else
    health_key=""
fi

# Include the watcher's own outward spool lines so they are never reconsidered.
total=0
[[ -f "$events" ]] && total="$(wc -l < "$events" | tr -d ' ')"
tmp="$spoken.new.$$"
printf 'events=%s\nhealth=%s\n' "$total" "$health_key" > "$tmp"
mv -f "$tmp" "$spoken"
