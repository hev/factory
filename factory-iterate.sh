#!/bin/bash
# factory-iterate.sh - run exactly one gaffer iteration, then exit.
#
# Usage:
#   ./factory-iterate.sh [--dry-run] [--force] [--host HOSTNAME] <instance>
#
# The one-shot runtime. Where factory-up.sh keeps a
# resident claude in a tmux pane and asks it to schedule its own next beat,
# this runs one iteration as a non-interactive `claude -p` process that runs to
# completion and exits. Liveness stops being a pgrep guess about a TUI's
# internal state and becomes an exit code and a timestamp.
#
# The scheduler owns the loop: launchd fires this every interval_base seconds,
# and the pacing hint the iteration returns decides which of those fires
# actually do work. Everything mechanical about closing an iteration -- the
# heartbeat, the beat line, the pacing hint -- is done here from the report,
# rather than by asking the model to remember.
#
# Exit codes:
#   0   iteration ran, or was correctly skipped (too early, already locked)
#   1   configuration or runtime failure
#   70  the iteration itself failed (claude errored, or returned no report)
#   78  wrong host, or this instance is not on the one-shot runtime

set -uo pipefail

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ADDENDUM="$ROOT_DIR/contracts/one-shot-addendum.md"
DRY_RUN=0
FORCE=0
HOST_OVERRIDE="${FACTORY_HOSTNAME_OVERRIDE:-}"

usage() { echo "Usage: $0 [--dry-run] [--force] [--host HOSTNAME] <instance>" >&2; }

expand_home() { printf '%s\n' "${1/#\~/$HOME}"; }

# Same reader factory-up.sh uses. Duplicated deliberately: the resident path is
# the rollback for this one, and it should not change while this is on trial.
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
        --dry-run) DRY_RUN=1; shift ;;
        --force)   FORCE=1; shift ;;
        --host)    [[ $# -ge 2 ]] || { usage; exit 2; }; HOST_OVERRIDE="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        -*)        usage; exit 2 ;;
        *)         INSTANCE="${1:-}"; shift; [[ $# -eq 0 ]] || { usage; exit 2; } ;;
    esac
done

INSTANCE="${INSTANCE:-}"
[[ -n "$INSTANCE" ]] || { usage; exit 2; }

CONFIG="$ROOT_DIR/factories/$INSTANCE.toml"
[[ -f "$CONFIG" ]] || { echo "factory config not found: $CONFIG" >&2; exit 1; }
[[ -f "$ADDENDUM" ]] || { echo "one-shot addendum not found: $ADDENDUM" >&2; exit 1; }

WORKDIR="$(expand_home "$(read_toml_string workspace_path "$CONFIG")")"
HOME_HOST="$(read_toml_string home_host "$CONFIG")"
LOOP_CONTRACT="$(expand_home "$(read_toml_string loop_contract "$CONFIG")")"
PLANS_REPO="$(read_toml_string plans_repo "$CONFIG")"
# The branch approved intent lives on. Frozen in the config by `factory init`,
# never read from whatever is checked out — a gaffer that followed the working
# tree would be redirected by any checkout somebody did for another reason.
# Absent means a config written before the field existed, which meant main.
PLANS_BRANCH="$(read_toml_string plans_branch "$CONFIG")"
PLANS_BRANCH="${PLANS_BRANCH:-main}"

# Where the operator approves. The team is the scope wall in Linear and the
# state is the entire approval signal, so both reach the agent in the task line
# rather than being something it has to go and read out of the config.
LINEAR_TEAM="$(read_toml_string linear_team "$CONFIG")"
LINEAR_APPROVED_STATE="$(read_toml_string linear_approved_state "$CONFIG")"
# Optional. Absent means the factory marks the same thing with a label.
LINEAR_REVIEW_STATE="$(read_toml_string linear_review_state "$CONFIG")"
LINEAR_BACKLOG_STATE="$(read_toml_string linear_backlog_state "$CONFIG")"
# MCP OAuth sessions are keyed by server name, so a machine with two Linear
# logins gives each factory the one its own workspace was registered under.
LINEAR_MCP_SERVER="$(read_toml_string linear_mcp_server "$CONFIG")"
LINEAR_MCP_SERVER="${LINEAR_MCP_SERVER:-linear}"

RUNTIME="$(read_toml_string runtime "$CONFIG")"
RUNTIME="${RUNTIME:-resident}"
MODEL="$(read_toml_string model "$CONFIG")"
MODEL="${MODEL:-${FACTORY_MODEL:-claude-fable-5}}"
EFFORT="$(read_toml_string effort "$CONFIG")"
INTERVAL_BASE="$(read_toml_string interval_base "$CONFIG")"
INTERVAL_BASE="${INTERVAL_BASE:-300}"
LOCK_STALE_MIN="$(read_toml_string lock_stale_minutes "$CONFIG")"
LOCK_STALE_MIN="${LOCK_STALE_MIN:-60}"

for required in WORKDIR HOME_HOST LOOP_CONTRACT PLANS_REPO LINEAR_TEAM LINEAR_APPROVED_STATE; do
    [[ -n "${!required}" ]] || { echo "missing required config value: $required" >&2; exit 1; }
done

# ── guards ────────────────────────────────────────────────────

if [[ "$RUNTIME" != "one-shot" ]]; then
    echo "factory-iterate: '$INSTANCE' is runtime='$RUNTIME'; the resident path owns it" >&2
    exit 78
fi

CURRENT_HOST="${HOST_OVERRIDE:-$(hostname -s 2>/dev/null || hostname)}"
CURRENT_HOST="$(printf '%s\n' "$CURRENT_HOST" | cut -d. -f1 | tr '[:upper:]' '[:lower:]')"
HOME_HOST="$(printf '%s\n' "$HOME_HOST" | tr '[:upper:]' '[:lower:]')"
if [[ "$CURRENT_HOST" != "$HOME_HOST" ]]; then
    echo "refusing to iterate factory '$INSTANCE': home_host is '$HOME_HOST', current host is '$CURRENT_HOST'" >&2
    exit 78
fi

[[ -d "$WORKDIR" ]] || { echo "workspace not found: $WORKDIR" >&2; exit 1; }
[[ -f "$LOOP_CONTRACT" ]] || { echo "loop contract not found: $LOOP_CONTRACT" >&2; exit 1; }
command -v claude &>/dev/null || { echo "claude not on PATH" >&2; exit 1; }
command -v jq &>/dev/null || { echo "jq not on PATH (needed to read the report)" >&2; exit 1; }

STATE_DIR="$HOME/.factory/iterations/$INSTANCE"
HEARTBEAT_FILE="$HOME/.factory/heartbeat/$INSTANCE"
LOCK_DIR="$STATE_DIR/lock"
HINT_FILE="$STATE_DIR/next-interval"
LAST_JSON="$STATE_DIR/last.json"
WAKE_FILE="$STATE_DIR/wake"
LOG_FILE="$STATE_DIR/iterations.log"
mkdir -p "$STATE_DIR" "$(dirname "$HEARTBEAT_FILE")"

now() { date +%s; }
mtime() { stat -f %m "$1" 2>/dev/null || echo 0; }
log() { printf '%s factory-iterate[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$INSTANCE" "$*"; }

# ── pacing gate ───────────────────────────────────────────────
# launchd fires on a fixed short interval so the hint has resolution. The hint
# the last iteration returned decides whether this fire does any work, which is
# how dynamic pacing survives without an agent having to remember it.

hint_interval() {
    local v
    [[ -f "$HINT_FILE" ]] || { echo "$INTERVAL_BASE"; return; }
    v="$(tr -dc '0-9' < "$HINT_FILE")"
    [[ -n "$v" && "$v" -ge "$INTERVAL_BASE" ]] && echo "$v" || echo "$INTERVAL_BASE"
}

# A P0 interrupt (scripts/gaffer-msg.sh) drops a wake flag here. It is the
# one-shot replacement for typing INTERRUPT into a resident gaffer's pane: it
# cannot start an iteration, it only makes the next scheduled fire ignore the
# pacing hint, which bounds P0 latency at interval_base instead of the hint.
WOKEN=0
if [[ -f "$WAKE_FILE" ]]; then
    WOKEN=1
    rm -f "$WAKE_FILE"
    log "woken by an interrupt; ignoring the pacing hint"
fi

INTERVAL="$(hint_interval)"
if [[ "$FORCE" -eq 0 && "$WOKEN" -eq 0 && -f "$HEARTBEAT_FILE" ]]; then
    age=$(( $(now) - $(mtime "$HEARTBEAT_FILE") ))
    if [[ "$age" -lt "$INTERVAL" ]]; then
        log "too early ($((age))s of ${INTERVAL}s); nothing to do"
        exit 0
    fi
fi

# ── tend the floor ────────────────────────────────────────────
# Before the model runs, not after: finished workers are already gone by the
# time the iteration reads the floor, and the stuck ones arrive in the prompt
# as a fact rather than something the gaffer has to go and notice. This is the
# half of step 6 that needs no judgment, so it never waits on one.

REAP_OUT="$("$ROOT_DIR/scripts/factory-reap.sh" "$INSTANCE" 2>/dev/null)"
REAPED="$(grep -c '^reaped' <<<"$REAP_OUT" || true)"
STUCK="$(grep -c '^stuck' <<<"$REAP_OUT" || true)"
[[ "$REAPED" -gt 0 || "$STUCK" -gt 0 ]] && log "reaped $REAPED, stuck $STUCK"

# ── the prompt ────────────────────────────────────────────────

SYSTEM_PROMPT="$(cat "$LOOP_CONTRACT"; printf '\n\n'; cat "$ADDENDUM")"
TASK="Run one factory parent iteration. Instance: $INSTANCE; plans repo: $PLANS_REPO; plans branch: $PLANS_BRANCH.
Linear team: $LINEAR_TEAM; approved state: $LINEAR_APPROVED_STATE. An RFC in that state is approved and nothing else is.
Queue states: review=${LINEAR_REVIEW_STATE:-<none, use a testing label>}; backlog=${LINEAR_BACKLOG_STATE:-<none, use a backlog label>}."
if [[ -n "$REAP_OUT" ]]; then
    TASK="$TASK

Worker sessions at the start of this beat (scripts/factory-reap.sh has already
run: reaped ones are gone and harvested, stuck ones are still up and are yours
to route at step 6):
$REAP_OUT"
fi

# Counters mirror scripts/factory-beat.sh's keys, so the beat line is a direct
# mapping rather than a translation. next_interval replaces ScheduleWakeup.
REPORT_SCHEMA='{
  "type": "object",
  "properties": {
    "summary":        {"type": "string"},
    "waiting_on_you": {"type": "array", "items": {"type": "string"}},
    "next_interval":  {"type": "integer"},
    "dispatched":     {"type": "integer"},
    "harvested":      {"type": "integer"},
    "prs_opened":     {"type": "integer"},
    "prs_merged":     {"type": "integer"},
    "self_merged":    {"type": "integer"},
    "approved":       {"type": "integer"},
    "learnings":      {"type": "integer"},
    "ready":          {"type": "integer"},
    "testing":        {"type": "integer"},
    "blocked":        {"type": "integer"},
    "quiet":          {"type": "boolean"}
  },
  "required": ["summary", "waiting_on_you", "next_interval"]
}'

# The beat gets exactly one MCP server and no others. Whatever else this
# machine has registered — other Linear logins, Slack, a browser — is somebody
# else's tooling, and loading it into every beat is tool definitions the gaffer
# pays for and never calls. The OAuth session is keyed by server name, so
# naming the same server here reuses the login `claude mcp add` already made.
MCP_CONFIG_FILE="$STATE_DIR/mcp.json"

CLAUDE_ARGS=(
    -p "$TASK"
    --append-system-prompt "$SYSTEM_PROMPT"
    --permission-mode bypassPermissions
    --mcp-config "$MCP_CONFIG_FILE"
    --strict-mcp-config
    --output-format json
    --json-schema "$REPORT_SCHEMA"
)
[[ -n "$MODEL"  ]] && CLAUDE_ARGS+=(--model "$MODEL")
[[ -n "$EFFORT" ]] && CLAUDE_ARGS+=(--effort "$EFFORT")

if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "would iterate factory '$INSTANCE'"
    echo "workspace=$WORKDIR"
    echo "contract=$LOOP_CONTRACT (+ $(basename "$ADDENDUM"))"
    echo "model=$MODEL effort=${EFFORT:-<inherit>}"
    echo "linear=$LINEAR_TEAM approved=$LINEAR_APPROVED_STATE via $LINEAR_MCP_SERVER"
    echo "interval=${INTERVAL}s (base ${INTERVAL_BASE}s)"
    echo "floor:"
    "$ROOT_DIR/scripts/factory-reap.sh" "$INSTANCE" --dry-run | sed 's/^/  /'
    echo "state=$STATE_DIR"
    exit 0
fi

cat > "$MCP_CONFIG_FILE" <<MCP
{"mcpServers":{"$LINEAR_MCP_SERVER":{"type":"http","url":"https://mcp.linear.app/mcp"}}}
MCP

# ── lock ──────────────────────────────────────────────────────
# mkdir is the atomic primitive available everywhere; macOS has no flock. A
# lock older than lock_stale_minutes is a crashed iteration, not a running one.

if ! mkdir "$LOCK_DIR" 2>/dev/null; then
    lock_age_min=$(( ( $(now) - $(mtime "$LOCK_DIR") ) / 60 ))
    if [[ "$lock_age_min" -ge "$LOCK_STALE_MIN" ]]; then
        log "clearing stale lock (${lock_age_min}m old)"
        rm -rf "$LOCK_DIR" && mkdir "$LOCK_DIR" 2>/dev/null || { log "could not take lock"; exit 1; }
    else
        log "an iteration is already running (${lock_age_min}m); nothing to do"
        exit 0
    fi
fi
trap 'rm -rf "$LOCK_DIR"' EXIT

# ── the iteration ─────────────────────────────────────────────

log "starting"
started="$(now)"

# The iteration runs in the background so its pid is recorded and it can be
# halted from outside: a P0 interrupt kills it, the wrapper falls through to
# the failure path without stamping the heartbeat, and the next fire re-reads
# the world fresh with the inbox message waiting at step 0.
OUT_FILE="$STATE_DIR/.out.$$"
( cd "$WORKDIR" && "$ROOT_DIR/scripts/factory-as.sh" gaffer -- \
    claude "${CLAUDE_ARGS[@]}" ) >"$OUT_FILE" 2>>"$LOG_FILE" &
claude_pid=$!
printf '%s\n' "$claude_pid" > "$LOCK_DIR/pid"
{ wait "$claude_pid"; status=$?; } 2>/dev/null
OUT="$(cat "$OUT_FILE" 2>/dev/null)"
rm -f "$OUT_FILE"
elapsed=$(( $(now) - started ))

# 143 = TERM. An iteration halted on purpose is not a fault; say so plainly and
# leave the heartbeat alone so the next fire runs immediately.
if [[ "$status" -eq 143 ]]; then
    log "iteration halted after ${elapsed}s (interrupt); next fire will drain the inbox"
    printf '%s iteration halted by interrupt after %ss\n' \
        "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$elapsed" >> "$LOG_FILE"
    exit 0
fi

if [[ "$status" -ne 0 || -z "$OUT" ]]; then
    log "claude exited $status after ${elapsed}s"
    printf '%s iteration failed (exit %s)\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$status" >> "$LOG_FILE"
    exit 70
fi

if ! jq -e . >/dev/null 2>&1 <<<"$OUT"; then
    log "claude returned output that is not JSON after ${elapsed}s"
    printf '%s\n' "$OUT" >> "$LOG_FILE"
    exit 70
fi

is_error="$(jq -r '.is_error // false' <<<"$OUT")"
report="$(jq -c '.structured_output // empty' <<<"$OUT")"

if [[ "$is_error" == "true" || -z "$report" ]]; then
    log "iteration returned no report after ${elapsed}s (is_error=$is_error)"
    printf '%s\n' "$OUT" >> "$LOG_FILE"
    exit 70
fi

printf '%s\n' "$OUT" > "$LAST_JSON"

# ── close-out (the contract's step 8, done here so it cannot be forgotten) ──

field() { jq -r --arg k "$1" '.[$k] // 0' <<<"$report"; }

"$ROOT_DIR/scripts/factory-beat.sh" "$INSTANCE" \
    dispatched="$(field dispatched)" \
    harvested="$(field harvested)" \
    prs_opened="$(field prs_opened)" \
    prs_merged="$(field prs_merged)" \
    self_merged="$(field self_merged)" \
    approved="$(field approved)" \
    learnings="$(field learnings)" \
    ready="$(field ready)" \
    testing="$(field testing)" \
    blocked="$(field blocked)" \
    quiet="$(jq -r 'if .quiet == true then 1 else 0 end' <<<"$report")" \
    waiting="$(jq -r '.waiting_on_you | length' <<<"$report")" \
    tokens="$(jq -r '.usage.output_tokens // 0' <<<"$OUT")" \
    cost_usd="$(jq -r '.total_cost_usd // 0' <<<"$OUT")" \
    reaped="$REAPED" \
    stuck="$STUCK" \
    duration_s="$elapsed" \
    turns="$(jq -r '.num_turns // 0' <<<"$OUT")" \
    runtime=one-shot || log "beat line failed to write"

next="$(jq -r '.next_interval // empty' <<<"$report")"
if [[ -n "$next" && "$next" -gt 0 ]]; then
    printf '%s\n' "$next" > "$HINT_FILE"
else
    printf '%s\n' "$INTERVAL_BASE" > "$HINT_FILE"
fi

# The heartbeat now records that an iteration *finished*, which is the thing
# the watchdog actually wants to know.
touch "$HEARTBEAT_FILE"

# A permission denial in print mode is silent otherwise: there is no prompt to
# answer, so the iteration just quietly did less than it was asked to.
denials="$(jq -r '.permission_denials | length' <<<"$OUT")"
[[ "$denials" -gt 0 ]] && log "WARNING: $denials permission denial(s) -- see $LAST_JSON"

{
    printf '\n=== %s (%ss, %s turns, $%s) ===\n' \
        "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$elapsed" \
        "$(jq -r '.num_turns // 0' <<<"$OUT")" "$(jq -r '.total_cost_usd // 0' <<<"$OUT")"
    printf 'session: %s\n' "$(jq -r '.session_id // "?"' <<<"$OUT")"
    jq -r '.summary' <<<"$report"
} >> "$LOG_FILE"

waiting_n="$(jq -r '.waiting_on_you | length' <<<"$report")"
log "done in ${elapsed}s; waiting on you: $waiting_n; next in $(cat "$HINT_FILE")s"
exit 0
