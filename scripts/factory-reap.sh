#!/bin/bash
# factory-reap.sh — tend this instance's worker sessions. Runs every beat.
#
# Usage: scripts/factory-reap.sh <instance> [--dry-run]
#
# A worker is a tmux session named worker-<instance>-<slug> with an entry in
# ~/.factory/children/ (contracts/child-ledger.md). When it finishes it does not
# exit: the harness sits at its own prompt with the work done behind it. That
# is why "a session sitting at a shell prompt is done" never fired — an
# interactive agent never reaches a shell prompt — and why finished workers
# used to sit on the floor for hours.
#
# The signal that does work is tmux's own #{window_activity}: the last time the
# pane produced output. A working agent redraws its status line every second, a
# finished one stops, and capture-pane does not bump it — so looking is free.
#
# Three outcomes, one line each on stdout:
#
#   reaped   idle past the threshold with its pull request already stamped, or
#            dropped back to a shell. The pane and its ledger entry are written
#            to ~/.factory/harvest/<instance>/<session>.log, then the session is
#            killed and the entry deleted.
#   stuck    idle past the threshold with no pull request. Never killed: that is
#            the gaffer's call at step 6 of the loop contract, and killing it
#            would throw away the only record of what went wrong.
#   live     working, or attached to by a human. Left alone.
#
# An attached session is never reaped whatever its state — somebody is reading
# it, and pulling a pane out from under them is not cleanup.
#
# Cleanup uncertainty retains the worktree and returns nonzero for the parent.

set -uo pipefail

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LEDGER_DIR="${FACTORY_LEDGER_DIR:-$HOME/.factory/children}"
HARVEST_ROOT="${FACTORY_HARVEST_DIR:-$HOME/.factory/harvest}"
DRY_RUN=0
CLEANUP_STATUS=0
CLEANUP_DIR=""

usage() { echo "Usage: $0 <instance> [--dry-run]" >&2; }

INSTANCE=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run) DRY_RUN=1; shift ;;
        -h|--help) usage; exit 0 ;;
        -*) echo "factory-reap: unknown flag: $1" >&2; usage; exit 2 ;;
        *)  if [[ -z "$INSTANCE" ]]; then INSTANCE="$1"; shift
            else echo "factory-reap: unexpected arg: $1" >&2; usage; exit 2; fi ;;
    esac
done
[[ -n "$INSTANCE" ]] || { usage; exit 2; }

CLEANUP_DIR="$HARVEST_ROOT/$INSTANCE/worktrees"
if [[ -n "${FACTORY_GAFFER_SESSION:-}" ]]; then
    [[ "$FACTORY_GAFFER_SESSION" =~ ^gaffer-[a-zA-Z0-9_-]+$ ]] || { echo "invalid gaffer session" >&2; exit 2; }
    CLEANUP_DIR="$CLEANUP_DIR/$FACTORY_GAFFER_SESSION"
fi
CONFIG="$ROOT_DIR/factories/$INSTANCE.toml"
[[ -f "$CONFIG" ]] || { echo "factory-reap: no config: $CONFIG" >&2; exit 1; }
command -v tmux &>/dev/null || { echo "factory-reap: tmux missing; cannot verify sessions" >&2; exit 1; }

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

if [[ "$(read_toml_string runtime "$CONFIG")" == sessions && "$DRY_RUN" == 0 && -z "${FACTORY_GAFFER_SESSION:-}" ]]; then
    echo "factory-reap: sessions runtime requires an owning FACTORY_GAFFER_SESSION" >&2
    exit 1
fi

# How long a pane may be silent before it is done rather than thinking. A
# working agent updates its pane every second, so this is generous by design:
# the cost of waiting is a stale row in the picker, and the cost of being wrong
# is a killed worker.
IDLE_MIN="$(read_toml_string idle_minutes "$CONFIG")"
IDLE_MIN="${IDLE_MIN:-${FACTORY_IDLE_MINUTES:-15}}"
[[ "$IDLE_MIN" =~ ^[0-9]+$ ]] || IDLE_MIN=15
IDLE_S=$(( IDLE_MIN * 60 ))

HARVEST_DIR="$HARVEST_ROOT/$INSTANCE"
NOW="$(date +%s)"

# Is this a worker of this instance? The ledger is authoritative and the naming
# convention is the fallback, so a worker dispatched without an entry is still
# tended. The gaffer (gaffer-<instance>) is deliberately not a worker: this
# script never reaps the thing that dispatches.
ledger_file() { printf '%s/%s.json\n' "$LEDGER_DIR" "$1"; }

ledger_field() {  # session key
    local file; file="$(ledger_file "$1")"
    [[ -f "$file" ]] || return 1
    command -v jq &>/dev/null || return 1
    jq -er --arg k "$2" '.[$k] // empty' "$file" 2>/dev/null
}

is_worker() {  # session
    local owner
    if [[ -n "${FACTORY_GAFFER_SESSION:-}" ]]; then
        [[ "$(ledger_field "$1" parent)" == "$FACTORY_GAFFER_SESSION" ]] || return 1
    fi
    case "$1" in
        gaffer-*) return 1 ;;   # never the things that dispatch
    esac
    owner="$(ledger_field "$1" instance)" && [[ "$owner" == "$INSTANCE" ]] && return 0
    [[ -f "$(ledger_file "$1")" ]] && return 1      # ledgered to somebody else
    case "$1" in
        worker-"$INSTANCE"-*) return 0 ;;
        *) return 1 ;;
    esac
}

# Is an agent still running in the pane? claude rewrites its process title to a
# version string, so this reads comm (what was executed) rather than args, and
# walks a few levels down because the agent is not always the shell's own child.
agent_running() {  # pid [depth]
    local pid="$1" depth="${2:-0}" kid
    [[ "$depth" -ge 4 ]] && return 1
    for kid in $(pgrep -P "$pid" 2>/dev/null); do
        case "$(ps -p "$kid" -o comm= 2>/dev/null)" in
            *claude*|*codex*|*aider*) return 0 ;;
        esac
        agent_running "$kid" $((depth + 1)) && return 0
    done
    return 1
}

dur() {
    local s=$1
    if   [[ $s -lt 60 ]];   then echo "${s}s"
    elif [[ $s -lt 3600 ]]; then echo "$((s / 60))m"
    else                         echo "$((s / 3600))h$(( (s % 3600) / 60 ))m"; fi
}

# Keep diagnostic output with the pane, including failures that scrolled away.
# Evidence itself lives until the plan's harvest-log sweep (loop step 7).
close_browser() {  # session
    local session="$1" log="$HARVEST_DIR/$1.log"
    local evidence="$HOME/.factory/evidence/$INSTANCE/$1"
    [[ -d "$evidence" || -n "$(read_toml_string preview_domains "$CONFIG")" ]] || return 0
    mkdir -p "$HARVEST_DIR"
    if [[ -f "$evidence/browser.log" ]]; then
        cat "$evidence/browser.log" >> "$log"
    fi
    if command -v agent-browser >/dev/null 2>&1; then
        if ! agent-browser --session "$session" close >> "$log" 2>&1; then
            printf '# agent-browser close failed\n' >> "$log"
            printf 'browser %-34s close failed — see %s\n' "$session" "$log" >&2
        fi
    else
        printf '# agent-browser missing; session cleanup unavailable\n' >> "$log"
        printf 'browser %-34s agent-browser missing — cleanup unavailable\n' "$session" >&2
    fi
}

harvest() {  # session idle_s note
    local session="$1" idle="$2" note="$3" log="$HARVEST_DIR/$1.log"
    if [[ "$DRY_RUN" -eq 1 ]]; then
        printf 'reaped  %-34s idle %s, %s (dry run)\n' "$session" "$(dur "$idle")" "$note"
        return
    fi
    mkdir -p "$HARVEST_DIR"
    {
        printf '# harvested %s by factory-reap (idle %s, %s)\n' \
            "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$(dur "$idle")" "$note"
        if [[ -f "$(ledger_file "$session")" ]]; then
            printf '# ledger:\n'
            sed 's/^/# /' "$(ledger_file "$session")"
        fi
        printf '\n'
        tmux capture-pane -t "$session" -p -S -2000 2>/dev/null
    } > "$log"
    close_browser "$session"
    if [[ -f "$(ledger_file "$session")" ]]; then
        local cwd
        cwd="$(tmux display-message -p -t "$session" '#{pane_current_path}' 2>/dev/null)"
        python3 "$ROOT_DIR/scripts/factory-clean-worktrees.py" remember \
            "$(ledger_file "$session")" "$CLEANUP_DIR" "$cwd" || { CLEANUP_STATUS=1; return; }
    fi
    tmux kill-session -t "$session" 2>/dev/null || { CLEANUP_STATUS=1; return; }
    rm -f "$(ledger_file "$session")"
    printf 'reaped  %-34s idle %s, %s → %s\n' "$session" "$(dur "$idle")" "$note" "$log"
}

# A CI handoff is paused work, not a finished or stuck worker. Keep its pane,
# ledger and checkout until the gaffer handles the completion and acknowledges.
ci_workers=""
shopt -s nullglob
for watch in "$HOME/.factory/ci/$INSTANCE/"*.json; do
    worker="$(jq -er --arg instance "$INSTANCE" 'select(.instance == $instance) | .worker' "$watch")" || {
        echo "factory-reap: cannot read CI watch $watch" >&2; exit 1;
    }
    ci_workers+="$worker"$'\n'
done
shopt -u nullglob
ci_waiting() { [[ $'\n'"$ci_workers" == *$'\n'"$1"$'\n'* ]]; }

# ── live sessions ─────────────────────────────────────────────

while IFS='|' read -r session activity attached; do
    [[ -z "$session" ]] && continue
    is_worker "$session" || continue

    if ci_waiting "$session"; then
        printf 'waiting %-34s registered CI handoff, no model polling\n' "$session"
        continue
    fi

    idle=$(( NOW - ${activity:-$NOW} ))
    [[ "$idle" -lt 0 ]] && idle=0

    if [[ "${attached:-0}" != "0" ]]; then
        printf 'live    %-34s attached — left alone\n' "$session"
        continue
    fi
    if [[ "$idle" -lt "$IDLE_S" ]]; then
        printf 'live    %-34s working, output %s ago\n' "$session" "$(dur "$idle")"
        continue
    fi

    pane_pid="$(tmux display-message -p -t "$session" '#{pane_pid}' 2>/dev/null || echo 0)"
    pr="$(ledger_field "$session" pr || true)"

    if [[ -n "$pr" ]]; then
        harvest "$session" "$idle" "pull request #$pr open"
    elif ! agent_running "${pane_pid:-0}"; then
        harvest "$session" "$idle" "agent exited, shell only"
    else
        # What it was working, not where it was filed: machine work has no
        # issue (contracts/queues.md), so the plan and step are the identity.
        plan="$(ledger_field "$session" plan || true)"
        step="$(ledger_field "$session" step || true)"
        where="${plan:+ — ${plan}${step:+: $step}}"
        [[ -z "$where" ]] && where=" — $(ledger_field "$session" issue_url || echo "no plan recorded")"
        printf 'stuck   %-34s idle %s, no pull request%s\n' \
            "$session" "$(dur "$idle")" "$where"
    fi
done < <(tmux list-sessions -F '#{session_name}|#{window_activity}|#{session_attached}' 2>/dev/null)

# ── ledger entries whose session is gone ──────────────────────
# A file with no session is a worker somebody killed by hand, or a harvest that
# died halfway. Either way the picker reads this directory, so it gets cleared.

shopt -s nullglob
for file in "$LEDGER_DIR"/*.json; do
    session="$(basename "$file" .json)"
    is_worker "$session" || continue
    tmux has-session -t "=$session" 2>/dev/null && continue
    if ci_waiting "$session"; then
        printf 'waiting %-34s CI handoff retained, session absent\n' "$session"
        continue
    fi
    if [[ "$DRY_RUN" -eq 1 ]]; then
        printf 'cleared %-34s ledger entry, no session (dry run)\n' "$session"
    else
        close_browser "$session"
        python3 "$ROOT_DIR/scripts/factory-clean-worktrees.py" remember "$file" "$CLEANUP_DIR" "" || { CLEANUP_STATUS=1; continue; }
        rm -f "$file"
        printf 'cleared %-34s ledger entry, no session\n' "$session"
    fi
done
shopt -u nullglob

if [[ "$DRY_RUN" == 1 ]]; then
    python3 "$ROOT_DIR/scripts/factory-clean-worktrees.py" sweep "$CONFIG" "$CLEANUP_DIR" --dry-run || CLEANUP_STATUS=1
else
    python3 "$ROOT_DIR/scripts/factory-clean-worktrees.py" sweep "$CONFIG" "$CLEANUP_DIR" || CLEANUP_STATUS=1
fi
exit "$CLEANUP_STATUS"
