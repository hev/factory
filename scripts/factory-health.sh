#!/bin/bash
# factory-health.sh — is each factory actually beating?
#
# Usage: factory-health.sh [instance ...]     (default: every configured instance)
#
# Reads each instance's state on its `home_host`, over ssh when that is not
# this machine. FACTORY_HEALTH_LOCAL=1 reads local files regardless.
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

# Liveness lives on the machine the factory runs on. Every file read below —
# heartbeat, beats, iterations — is written by the gaffer under its own $HOME
# on the host named by `home_host`, so reading them anywhere else reports that
# machine's silence as the factory's: a laptop holding a clone and no gaffer
# says LATE about a factory beating perfectly well on the mini. Off-home,
# therefore, ask home_host over ssh rather than guessing from local files.
# FACTORY_HEALTH_LOCAL=1 forces the local read and is set on the remote hop, so
# a hostname that never matches costs one failed ssh instead of a loop.
home_host_of() {
    local config="$ROOT_DIR/factories/$1.toml" value=""
    [[ -f "$config" ]] && value="$(read_toml_string home_host "$config")"
    printf '%s\n' "$value" | cut -d. -f1 | tr '[:upper:]' '[:lower:]'
}

if [[ -z "${FACTORY_HEALTH_LOCAL:-}" ]]; then
    here="$(printf '%s' "${FACTORY_HOSTNAME_OVERRIDE:-$(hostname -s 2>/dev/null || hostname)}" | cut -d. -f1 | tr '[:upper:]' '[:lower:]')"

    hosts=""
    for instance in "${instances[@]}"; do
        h="$(home_host_of "$instance")"; h="${h:-$here}"
        case " $hosts " in *" $h "*) ;; *) hosts="$hosts $h" ;; esac
    done

    # Every instance is at home here: fall through and read the local files,
    # which is the whole of the common case and costs nothing.
    if [[ "$hosts" != " $here" ]]; then
        rc=0
        for h in $hosts; do
            group=""
            for instance in "${instances[@]}"; do
                ih="$(home_host_of "$instance")"; ih="${ih:-$here}"
                [[ "$ih" == "$h" ]] && group="$group $instance"
            done

            if [[ "$h" == "$here" ]]; then
                FACTORY_HEALTH_LOCAL=1 "$ROOT_DIR/scripts/factory-health.sh"$group || rc=1
                continue
            fi

            printf '# %s — read over ssh, where%s actually run\n' "$h" "$group"
            ssh -o BatchMode=yes -o ConnectTimeout=5 "$h" \
                "FACTORY_HEALTH_LOCAL=1 \"\$(cat ~/.factory/root)/scripts/factory-health.sh\"$group"
            status=$?
            # 255 is ssh itself failing, not a late factory. Saying which it
            # was matters: "the mini is unreachable" and "the mini says LATE"
            # are different problems with different fixes.
            if [[ "$status" -eq 255 ]]; then
                printf '%-12s %s\n' machine "UNREACHABLE $h — cannot read the factories that live there"
                rc=1
            elif [[ "$status" -ne 0 ]]; then
                rc=1
            fi
        done
        exit "$rc"
    fi
fi

now=$(date +%s)
unhealthy=0

# Shared floor dependencies. These are reported here so the timer-driven
# watcher can speak once before the next several workers fail the same way.
for required in jq gh; do
    if ! command -v "$required" >/dev/null 2>&1; then
        printf '%-12s %s\n' machine "MISSING required command: $required"
        unhealthy=1
    fi
done
if command -v gh >/dev/null 2>&1 && ! gh auth status >/dev/null 2>&1; then
    printf '%-12s %s\n' machine "AUTH GitHub login is unavailable or expired"
    unhealthy=1
fi
disk_floor="${FACTORY_DISK_FREE_PERCENT:-5}"
disk_free="$(df -Pk "$HOME" 2>/dev/null | awk 'NR==2 {gsub(/%/,"",$5); print 100-$5}')"
if [[ "$disk_floor" =~ ^[0-9]+$ && "$disk_free" =~ ^[0-9]+$ && "$disk_free" -lt "$disk_floor" ]]; then
    printf '%-12s %s\n' machine "DISK only ${disk_free}% free under $HOME"
    unhealthy=1
fi

for instance in "${instances[@]}"; do
    config="$ROOT_DIR/factories/$instance.toml"
    [[ -f "$config" ]] || { printf '%-12s %s\n' "$instance" "no config"; unhealthy=1; continue; }

    runtime="$(read_toml_string runtime "$config")"; runtime="${runtime:-resident}"
    base="$(read_toml_string interval_base "$config")"; base="${base:-300}"
    state="$HOME/.factory/iterations/$instance"
    hb="$HOME/.factory/heartbeat/$instance"

    # Ticks are paced by interval_base alone; the model-returned pacing hint
    # is gone with the sensor, so lateness is measured against the base.
    interval="$base"
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
