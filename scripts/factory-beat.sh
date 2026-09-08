#!/bin/bash
# factory-beat.sh — append one structured beat line to the instance beat log.
#
# Usage: factory-beat.sh <instance> [key=value ...]
#
# The loop contract calls this once per iteration (step 8), right after the
# heartbeat touch. Numeric values are emitted as JSON numbers, anything else
# as a string; the script stamps the UTC timestamp. The resulting
# ~/.factory/beats/<instance>.jsonl is the deterministic substrate metrics
# and retros read — models never reconstruct these numbers from memory.

set -euo pipefail

INSTANCE="${1:?usage: factory-beat.sh <instance> [key=value ...]}"
shift

BEAT_DIR="${FACTORY_BEAT_DIR:-$HOME/.factory/beats}"
mkdir -p "$BEAT_DIR"

line="{\"ts\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"instance\":\"$INSTANCE\""
has_api=0 has_sub=0
for kv in "$@"; do
    key="${kv%%=*}"
    [[ "$key" == "api_usd" ]] && has_api=1
    [[ "$key" == "sub_usd" ]] && has_sub=1
    value="${kv#*=}"
    if [[ "$value" != "null" && ! "$value" =~ ^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$ ]]; then
        value="\"${value//\"/\\\"}\""
    fi
    line+=",\"$key\":$value"
done
[[ "$has_api" -eq 0 ]] && line+=',"api_usd":null'
[[ "$has_sub" -eq 0 ]] && line+=',"sub_usd":null'
line+="}"

printf '%s\n' "$line" >> "$BEAT_DIR/$INSTANCE.jsonl"
