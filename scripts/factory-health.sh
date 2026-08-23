#!/bin/bash
# factory-health.sh — is each factory actually beating?
#
# Usage: factory-health.sh [instance ...]     (default: every configured instance)
#
# Under the one-shot runtime, liveness is a timestamp
# rather than a process: `~/.factory/heartbeat/<instance>` is touched when an
# iteration *finishes*. A factory is late when that stamp is older than three
# times the interval its last iteration asked for, or fifteen minutes,
# whichever is longer. The floor is what stops a short interval crying wolf: an
# iteration that takes longer than the beat it was scheduled on is ordinary,
# not late. The larger of the pair still surfaces a dead loop inside one report
# cycle.
#
# Exit 0 when everything is healthy, 1 when anything is late, so launchd or a
# sweep can alert on the exit code without parsing the output.

set -uo pipefail
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LATE_FACTOR="${FACTORY_LATE_FACTOR:-3}"
LATE_FLOOR="${FACTORY_LATE_FLOOR:-900}"

read_toml_string() {
    awk -F= -v key="$1" '
        $1 ~ "^[[:space:]]*" key "[[:space:]]*$" {
            v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*#.*/,"",v)
            sub(/[[:space:]]*$/,"",v); gsub(/^"|"$/,"",v); print v; exit
        }' "$2"
}

dur() {
    local s=$1
    if   [[ $s -lt 60 ]];    then echo "${s}s"
    elif [[ $s -lt 3600 ]];  then echo "$((s/60))m"
    elif [[ $s -lt 86400 ]]; then echo "$((s/3600))h"
    else                          echo "$((s/86400))d"; fi
}

instances=("$@")
if [[ ${#instances[@]} -eq 0 ]]; then
    shopt -s nullglob
    for c in "$ROOT_DIR"/factories/*.toml; do
        i="$(basename "$c" .toml)"
        [[ "$i" == "example" ]] && continue
        instances+=("$i")
    done
    shopt -u nullglob
fi

# A machine with no factory configured is the fresh-clone state, not a fault:
# reception is up and about to walk somebody through the first one. Say so and
# exit clean, rather than tripping over an empty array under `set -u`.
if [[ ${#instances[@]} -eq 0 ]]; then
    echo "no factories configured — ask reception to set one up, or ./factory init --help"
    exit 0
fi

now=$(date +%s)
unhealthy=0

for instance in "${instances[@]}"; do
    config="$ROOT_DIR/factories/$instance.toml"
    [[ -f "$config" ]] || { printf '%-12s %s\n' "$instance" "no config"; unhealthy=1; continue; }

    runtime="$(read_toml_string runtime "$config")"; runtime="${runtime:-resident}"
    base="$(read_toml_string interval_base "$config")"; base="${base:-300}"
    state="$HOME/.factory/iterations/$instance"
    hb="$HOME/.factory/heartbeat/$instance"

    interval="$base"
    if [[ -f "$state/next-interval" ]]; then
        v="$(tr -dc '0-9' < "$state/next-interval")"
        [[ -n "$v" && "$v" -gt "$interval" ]] && interval="$v"
    fi
    late_after=$(( interval * LATE_FACTOR ))
    [[ "$late_after" -lt "$LATE_FLOOR" ]] && late_after="$LATE_FLOOR"

    if [[ ! -f "$hb" ]]; then
        printf '%-12s %-9s never beat\n' "$instance" "$runtime"
        unhealthy=1
        continue
    fi

    # factory-up.sh touches the heartbeat when it boots a loop, so on a factory
    # that has not finished an iteration yet the mtime is boot time and reads
    # as a fresh beat. The beat log is the honest record: no file, no completed
    # iteration, whatever the heartbeat says.
    if [[ ! -f "$HOME/.factory/beats/$instance.jsonl" ]]; then
        printf '%-12s %-9s booted, no completed beat yet\n' "$instance" "$runtime"
        continue
    fi

    age=$(( now - $(stat -f %m "$hb" 2>/dev/null || echo 0) ))
    waiting="-"
    [[ -f "$state/last.json" ]] && command -v jq &>/dev/null && \
        waiting="$(jq -r '.structured_output.waiting_on_you | length' "$state/last.json" 2>/dev/null || echo -)"

    # A resident beat that runs long is not a late beat: the heartbeat records
    # when an iteration *finished*, and the gaffer's pane says whether one is
    # running right now. Same signal the reaper uses on workers — an agent
    # redraws its pane every second while it works.
    in_flight=0
    if [[ "$runtime" == "resident" ]] && command -v tmux &>/dev/null; then
        pane_act="$(tmux display-message -p -t "gaffer-$instance" '#{window_activity}' 2>/dev/null || echo 0)"
        [[ "$pane_act" =~ ^[0-9]+$ ]] && [[ $(( now - pane_act )) -lt 120 ]] && in_flight=1
    fi

    # What the resident session is carrying, so the ceiling in the config is a
    # tuned number rather than a guess. Never a health verdict on its own: a
    # gaffer at the ceiling is not sick, it is due for a clear on its next
    # quiet beat, and one-shot has no context to report.
    ctx="-"
    if [[ "$runtime" == "resident" ]]; then
        ctx_raw="$("$ROOT_DIR/scripts/factory-context.sh" "$instance" 2>/dev/null || true)"
        [[ "$ctx_raw" =~ ^[0-9]+$ ]] && ctx="$(( ctx_raw / 1000 ))k"
    fi

    if [[ "$in_flight" -eq 1 ]]; then
        printf '%-12s %-9s ok    beat in flight, last finished %s ago  waiting:%s  ctx:%s\n' \
            "$instance" "$runtime" "$(dur "$age")" "$waiting" "$ctx"
    elif [[ "$age" -gt "$late_after" ]]; then
        printf '%-12s %-9s LATE  last beat %s ago (expected within %s)  waiting:%s  ctx:%s\n' \
            "$instance" "$runtime" "$(dur "$age")" "$(dur "$late_after")" "$waiting" "$ctx"
        unhealthy=1
    else
        printf '%-12s %-9s ok    last beat %s ago  waiting:%s  ctx:%s\n' \
            "$instance" "$runtime" "$(dur "$age")" "$waiting" "$ctx"
    fi

    if [[ -d "$state/lock" ]]; then
        lock_age=$(( now - $(stat -f %m "$state/lock" 2>/dev/null || echo 0) ))
        printf '%-12s %-9s       iteration in flight (%s)\n' "$instance" "" "$(dur "$lock_age")"
    fi
done

exit "$unhealthy"
