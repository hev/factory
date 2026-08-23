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
for kv in "$@"; do
    key="${kv%%=*}"
    value="${kv#*=}"
    case "$value" in
        ''|*[!0-9.-]*) value="\"${value//\"/\\\"}\"" ;;
    esac
    line+=",\"$key\":$value"
done
line+="}"

printf '%s\n' "$line" >> "$BEAT_DIR/$INSTANCE.jsonl"
