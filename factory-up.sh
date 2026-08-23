#!/bin/bash
# factory-up.sh - Ensure a factory parent agent is running for one instance.
#
# Usage:
#   ./factory-up.sh [--dry-run] [--host HOSTNAME] <instance>
#
# The instance config references an existing workspace. This script refuses to
# boot on any host other than the configured home_host, preventing a second
# parent from starting on a laptop or stale clone.

set -euo pipefail

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRY_RUN=0
HOST_OVERRIDE="${FACTORY_HOSTNAME_OVERRIDE:-}"

usage() {
    echo "Usage: $0 [--dry-run] [--host HOSTNAME] <instance>" >&2
}

expand_home() {
    local value="$1"
    printf '%s\n' "${value/#\~/$HOME}"
}

read_toml_string() {
    local key="$1" file="$2"
    awk -F= -v key="$key" '
        $1 ~ "^[[:space:]]*" key "[[:space:]]*$" {
            value=$2
            sub(/^[[:space:]]*/, "", value)
            sub(/[[:space:]]*#.*/, "", value)
            sub(/[[:space:]]*$/, "", value)
            gsub(/^"|"$/, "", value)
            print value
            exit
        }
    ' "$file"
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run)
            DRY_RUN=1
            shift
            ;;
        --host)
            [[ $# -ge 2 ]] || { usage; exit 2; }
            HOST_OVERRIDE="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        -*)
            usage
            exit 2
            ;;
        *)
            INSTANCE="${1:-}"
            shift
            [[ $# -eq 0 ]] || { usage; exit 2; }
            ;;
    esac
done

INSTANCE="${INSTANCE:-}"
[[ -n "$INSTANCE" ]] || { usage; exit 2; }

CONFIG="$ROOT_DIR/factories/$INSTANCE.toml"
[[ -f "$CONFIG" ]] || { echo "factory config not found: $CONFIG" >&2; exit 1; }

# The gaffer's session name is derived, never configured: <role>-<instance>,
# the same rule reception and every worker follow.
SESSION="gaffer-$INSTANCE"
WORKDIR="$(expand_home "$(read_toml_string workspace_path "$CONFIG")")"
HOME_HOST="$(read_toml_string home_host "$CONFIG")"
LOOP_CONTRACT="$(expand_home "$(read_toml_string loop_contract "$CONFIG")")"
PLANS_REPO="$(read_toml_string plans_repo "$CONFIG")"
# See factory-iterate.sh: frozen in the config, defaulting to what a config
# written before the field existed meant.
PLANS_BRANCH="$(read_toml_string plans_branch "$CONFIG")"
PLANS_BRANCH="${PLANS_BRANCH:-main}"

# Optional per-instance reasoning effort. Without it the parent inherits the
# machine-global ~/.claude/settings.json effortLevel (xhigh on the mini).
EFFORT="$(read_toml_string effort "$CONFIG")"

# The model the gaffer itself runs on, defaulting to Fable: a parent dispatches,
# verifies and reports, and the moments that need more than that are handed to a
# subagent or a worker, each of which picks its own model. FACTORY_MODEL is the
# machine-wide override; the per-instance answer is `model` in the config.
MODEL="$(read_toml_string model "$CONFIG")"
MODEL="${MODEL:-${FACTORY_MODEL:-claude-fable-5}}"
CLAUDE_CMD="claude --model $MODEL${EFFORT:+ --effort $EFFORT}"

for required in WORKDIR HOME_HOST LOOP_CONTRACT PLANS_REPO; do
    [[ -n "${!required}" ]] || { echo "missing required config value: $required" >&2; exit 1; }
done

CURRENT_HOST="${HOST_OVERRIDE:-$(hostname -s 2>/dev/null || hostname)}"
CURRENT_HOST="$(printf '%s\n' "$CURRENT_HOST" | cut -d. -f1 | tr '[:upper:]' '[:lower:]')"
HOME_HOST="$(printf '%s\n' "$HOME_HOST" | tr '[:upper:]' '[:lower:]')"

if [[ "$CURRENT_HOST" != "$HOME_HOST" ]]; then
    echo "refusing to boot factory '$INSTANCE': home_host is '$HOME_HOST', current host is '$CURRENT_HOST'" >&2
    exit 78
fi

[[ -d "$WORKDIR" ]] || { echo "workspace not found: $WORKDIR" >&2; exit 1; }
[[ -f "$LOOP_CONTRACT" ]] || { echo "loop contract not found: $LOOP_CONTRACT" >&2; exit 1; }

LOOP_CMD="/loop Run the factory parent iteration in $LOOP_CONTRACT - follow it exactly. Instance: $INSTANCE; plans repo: $PLANS_REPO; plans branch: $PLANS_BRANCH."

# Loop-liveness heartbeat: the contract requires each iteration to touch this
# file; factory-up treats staleness (+ an idle pane) as a wedged parent.
HEARTBEAT_FILE="$HOME/.factory/heartbeat/$INSTANCE"
HEARTBEAT_STALE_MIN="$(read_toml_string heartbeat_stale_minutes "$CONFIG")"
HEARTBEAT_STALE_MIN="${HEARTBEAT_STALE_MIN:-30}"

# Context ceiling for a resident gaffer. A resident session carries every beat
# it has ever run, and the contract says a beat reads the world fresh — so that
# history is not memory, it is a bill: every beat re-reads the whole context,
# busy or quiet, and a gaffer left alone for a day reaches half a million
# tokens. The agent cannot fix this itself (`/clear` is a harness command, not
# something an agent can invoke), so the sweep lives out here with the other
# things the machine does on its own timer. 0 disables.
CONTEXT_THRESHOLD="$(read_toml_string context_clear_threshold "$CONFIG")"
CONTEXT_THRESHOLD="${FACTORY_CONTEXT_THRESHOLD:-${CONTEXT_THRESHOLD:-200000}}"

# Tend the floor before touching the gaffer. A resident loop schedules its own
# beat and can wedge; reaping here means finished workers still leave the floor
# on the machine's timer, whatever the agent is doing. Idempotent, and quiet
# when there is nothing to do.
if [[ "$DRY_RUN" -eq 0 ]]; then
    "$ROOT_DIR/scripts/factory-reap.sh" "$INSTANCE" 2>/dev/null | grep -v '^live' || true
fi

if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "would boot factory '$INSTANCE'"
    echo "session=$SESSION"
    echo "workspace=$WORKDIR"
    echo "home_host=$HOME_HOST"
    echo "loop_contract=$LOOP_CONTRACT"
    exit 0
fi

# The boot handoff must be verified, not timed. A blind sleep loses the race
# whenever the TUI is slow (post-reboot, first launch after an update): the
# loop command lands in a not-yet-ready input box and the parent sits at an
# unsubmitted prompt forever (observed 2026-07-11, after the
# 01:44 reboot).
pane() { tmux capture-pane -t "$SESSION" -p 2>/dev/null; }

# Submit LOOP_CMD into an already-prompting claude and verify it took: an
# unsubmitted command just sits in the input box; once claude accepts it the
# session shows activity ('⏺' response marker or the 'esc to interrupt'
# spinner). Extra Enters on an already-running session are harmless no-ops.
submit_loop_cmd() {
    tmux send-keys -t "$SESSION" "$LOOP_CMD"
    sleep 1
    for _ in $(seq 1 5); do
        tmux send-keys -t "$SESSION" Enter
        sleep 4
        if pane | grep -qE '⏺|esc to interrupt'; then
            echo "factory-up: loop running in '$SESSION'"
            mkdir -p "$(dirname "$HEARTBEAT_FILE")" && touch "$HEARTBEAT_FILE"
            return 0
        fi
    done
    echo "factory-up: loop command not confirmed submitted in '$SESSION'" >&2
    return 1
}

# Was the last beat a quiet one? The contract's own definition (rule: "a quiet
# beat is a cheap beat") is the condition worth clearing on — nothing moved, so
# nothing in the pane is mid-flight and nothing is about to be reported.
#
# The gaffer states it directly as `quiet=1`. Beats written before that field
# existed fall back to the movement counters summing to zero; the standing
# counts (testing, blocked, waiting) are deliberately not in that sum, because
# they describe the world rather than what this beat did to it.
last_beat_quiet() {
    local log="${FACTORY_BEAT_DIR:-$HOME/.factory/beats}/$INSTANCE.jsonl"
    [[ -s "$log" ]] || return 1
    tail -1 "$log" | jq -e '
        if has("quiet") then .quiet == 1
        else ((.dispatched // 0) + (.harvested // 0) + (.prs_opened // 0)
              + (.prs_merged // 0) + (.self_merged // 0) + (.approved // 0)
              + (.learnings // 0)) == 0
        end
    ' >/dev/null 2>&1
}

# Clear a resident gaffer that has grown past the ceiling, then put the loop
# back. Every guard here is a way of not clearing: unmeasurable context, a beat
# in flight, a beat that did something, or room to spare. The order matters —
# the cheap local checks run before the transcript scan.
#
# `/clear` takes the pending loop with it: the pane comes back to an empty
# prompt and no beat is scheduled. Re-submitting is therefore mandatory, not
# belt-and-braces, and it goes through the same submit_loop_cmd the wedge path
# uses so a clear that fails to restart is reported the same way.
maybe_clear_context() {
    [[ "$CONTEXT_THRESHOLD" =~ ^[0-9]+$ ]] || return 0
    [[ "$CONTEXT_THRESHOLD" -gt 0 ]] || return 0

    # Never mid-beat. Clearing a working gaffer throws away a beat's work and
    # can strand a worker it had just dispatched but not yet recorded.
    if pane | grep -q 'esc to interrupt'; then return 0; fi

    last_beat_quiet || return 0

    local ctx
    ctx="$("$ROOT_DIR/scripts/factory-context.sh" "$INSTANCE" 2>/dev/null)" || return 0
    [[ "$ctx" =~ ^[0-9]+$ ]] || return 0
    [[ "$ctx" -ge "$CONTEXT_THRESHOLD" ]] || return 0

    echo "factory-up: '$SESSION' carrying ${ctx} tokens (ceiling ${CONTEXT_THRESHOLD}) on a quiet beat; clearing"
    tmux send-keys -t "$SESSION" "/clear" Enter
    sleep 3
    submit_loop_cmd
}

start_parent() {
    tmux send-keys -t "$SESSION" "$CLAUDE_CMD" Enter

    # Wait for the claude input prompt before typing anything.
    local ready=0
    for _ in $(seq 1 30); do
        sleep 2
        if pane | grep -q '❯'; then ready=1; break; fi
    done
    if [[ "$ready" -ne 1 ]]; then
        echo "factory-up: claude TUI never showed a prompt in '$SESSION'" >&2
        return 1
    fi

    submit_loop_cmd
}

if tmux has-session -t "$SESSION" 2>/dev/null; then
    pane_pid="$(tmux display-message -p -t "$SESSION" '#{pane_pid}' 2>/dev/null || true)"
    if [[ -n "$pane_pid" ]] && pgrep -P "$pane_pid" >/dev/null 2>&1; then
        # Parent process is alive — but is the loop? The /loop lives inside
        # the claude harness, so a wedged session (crashed loop, lost wakeup)
        # looks identical to a healthy one from the process table. The loop
        # contract touches HEARTBEAT_FILE every iteration; a stale heartbeat
        # plus an idle pane (no spinner) means the parent is wedged, and we
        # re-send the loop command into the same session — history and
        # shoulder-surfing preserved, no restart.
        rekicked=0
        if [[ -f "$HEARTBEAT_FILE" ]]; then
            hb_age_min=$(( ( $(date +%s) - $(stat -f %m "$HEARTBEAT_FILE") ) / 60 ))
            if [[ "$hb_age_min" -ge "$HEARTBEAT_STALE_MIN" ]] \
                && ! pane | grep -q 'esc to interrupt'; then
                echo "factory-up: '$SESSION' wedged (heartbeat ${hb_age_min}m old, pane idle); re-sending loop command"
                submit_loop_cmd
                rekicked=1
            fi
        fi
        # A session that was just re-kicked is starting a beat, not sitting on
        # a quiet one — leave the ceiling to the next run of this script.
        if [[ "$rekicked" -eq 0 ]]; then
            maybe_clear_context || true
        fi
        exit 0
    fi
    echo "factory session '$SESSION' idle (parent exited); restarting parent"
    start_parent
    exit 0
fi

echo "creating factory session '$SESSION'"
# The gaffer's identity is decided here and nowhere else: it runs `gh` itself,
# from inside the session, so the session's environment is the only place the
# answer can live (contracts/extending.md §2).
"$ROOT_DIR/scripts/factory-as.sh" gaffer -- \
    tmux new-session -d -s "$SESSION" -c "$WORKDIR"
start_parent
