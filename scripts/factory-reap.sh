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
# Owner-scoped durable completion permits terminal harvest. Activity age and PR
# existence are diagnostics only. CI, attachments, identity conflicts and known
# busy/refusal prompts veto harvest. Evidence and worktree cleanup stay separate.

set -uo pipefail

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="${FACTORY_STATE_DIR:-$HOME/.factory}"
LEDGER_DIR="${FACTORY_LEDGER_DIR:-$STATE_DIR/children}"
HARVEST_ROOT="${FACTORY_HARVEST_DIR:-$STATE_DIR/harvest}"
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
[[ -f "$ROOT_DIR/scripts/factory-worker-completion.py" ]] || { echo "factory-reap: completion helper missing" >&2; exit 1; }
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
    local evidence="$STATE_DIR/evidence/$INSTANCE/$1"
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
    } >> "$log"
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
for watch in "$STATE_DIR/ci/$INSTANCE/"*.json; do
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

    # A reservation claims a task name, not an existing terminal. Event-mode
    # workers must prove that this terminal was created for that reservation,
    # even when launch failed or the controller crashed before recording it.
    if [[ -n "$(ledger_field "$session" task_id || true)" || -n "$(ledger_field "$session" launch_identity || true)" ]]; then
        launch_identity="$(ledger_field "$session" launch_identity || true)"
        terminal_identity="$(tmux show-environment -t "=$session" FACTORY_TASK_LAUNCH 2>/dev/null)" || terminal_identity=""
        if [[ -z "$launch_identity" || "$terminal_identity" != "FACTORY_TASK_LAUNCH=$launch_identity" ]]; then
            printf 'foreign %-34s reserved launch identity unverified — owner must inspect original launch; left alone\n' "$session"
            continue
        fi
    fi

    if ci_waiting "$session"; then
        printf 'waiting %-34s registered CI handoff — owner must handle and acknowledge the watch; no model polling\n' "$session"
        continue
    fi

    idle=$(( NOW - ${activity:-$NOW} ))
    [[ "$idle" -lt 0 ]] && idle=0

    if [[ "${attached:-0}" != "0" ]]; then
        printf 'live    %-34s attached — owner must wait for reader to detach; left alone\n' "$session"
        continue
    fi
    # A fresh redraw is not work. Only explicit testimony can establish
    # completion; the visible prompt can veto it but never supply it.
    pane="$(tmux capture-pane -t "=$session" -p -S -12 2>/dev/null)" || {
        printf 'stuck   %s owner %s: cannot read pane; inspect terminal before recovery\n' "$session" "${FACTORY_GAFFER_SESSION:-unknown}"
        CLEANUP_STATUS=1; continue
    }
    proof="$(printf '%s' "$pane" | python3 "$ROOT_DIR/scripts/factory-worker-completion.py" \
        "$(ledger_file "$session")" "$INSTANCE" "${FACTORY_GAFFER_SESSION:-}" "$CONFIG" )"
    completion_status=$?
    if [[ "$completion_status" == 0 ]]; then
        harvest "$session" "$idle" "owned task completed; evidence: $proof"
    elif [[ "$completion_status" == 2 ]]; then
        printf 'stuck   %s owner %s: %s\n' "$session" "${FACTORY_GAFFER_SESSION:-unknown}" "$proof"
    else
        printf 'stuck   %s owner %s: completion probe failed; inspect ledger/spool before recovery\n' "$session" "${FACTORY_GAFFER_SESSION:-unknown}"
        CLEANUP_STATUS=1
    fi

done < <(tmux list-sessions -F '#{session_name}|#{window_activity}|#{session_attached}' 2>/dev/null)

# ── ledger entries whose session is gone ──────────────────────
# An absent session is not completion. Retain unresolved ledgers; archive the
# ledger and testimony before clearing an explicitly completed worker.

shopt -s nullglob
for file in "$LEDGER_DIR"/*.json; do
    session="$(basename "$file" .json)"
    is_worker "$session" || continue
    tmux has-session -t "=$session" 2>/dev/null && continue
    if ci_waiting "$session"; then
        printf 'waiting %-34s CI handoff retained, session absent\n' "$session"
        continue
    fi
    proof="$(python3 "$ROOT_DIR/scripts/factory-worker-completion.py" "$file" "$INSTANCE" "${FACTORY_GAFFER_SESSION:-}" "$CONFIG" </dev/null)"
    completion_status=$?
    if [[ "$completion_status" != 0 ]]; then
        printf 'stuck   %s owner %s: absent session retained; %s\n' "$session" "${FACTORY_GAFFER_SESSION:-unknown}" "$proof"
        [[ "$completion_status" == 2 ]] || CLEANUP_STATUS=1
        continue
    fi
    if [[ "$DRY_RUN" -eq 1 ]]; then
        printf 'cleared %-34s ledger entry, no session (dry run)\n' "$session"
    else
        mkdir -p "$HARVEST_DIR"
        { printf '# absent completed worker; evidence: %s\n# ledger:\n' "$proof"; cat "$file"; } >> "$HARVEST_DIR/$session.log"
        close_browser "$session"
        python3 "$ROOT_DIR/scripts/factory-clean-worktrees.py" remember "$file" "$CLEANUP_DIR" "" || { CLEANUP_STATUS=1; continue; }
        rm -f "$file"
        printf 'cleared %-34s ledger entry, no session\n' "$session"
    fi
done
shopt -u nullglob

if [[ "${FACTORY_DEFER_WORKTREE_CLEANUP:-0}" == 1 ]]; then
    # A task-list assignment can still need an idle lane after an earlier PR
    # merged. Keep candidates until the owning controller records delivery.
    exit "$CLEANUP_STATUS"
fi

if [[ "$DRY_RUN" == 1 ]]; then
    python3 "$ROOT_DIR/scripts/factory-clean-worktrees.py" sweep "$CONFIG" "$CLEANUP_DIR" --dry-run || CLEANUP_STATUS=1
else
    python3 "$ROOT_DIR/scripts/factory-clean-worktrees.py" sweep "$CONFIG" "$CLEANUP_DIR" || CLEANUP_STATUS=1
fi
exit "$CLEANUP_STATUS"
