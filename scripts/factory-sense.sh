#!/bin/bash
# factory-sense.sh — the deterministic half of a beat: has anything moved?
#
# Usage:
#   scripts/factory-sense.sh <instance> [--state FILE]
#
# The kube-controller shape: desired state lives in plans/active/ and the
# queues, observed state is the floor, the inbox, and the board — and this
# script is the watch layer. It reads every surface the loop contract's sensor
# steps read, deterministically, and reports whether a model is needed at all.
# factory-iterate.sh runs it on every fire; a tick with no reasons is closed
# by the wrapper for zero model invocations, and a tick with reasons hands
# them to the beat as the reason it fired.
#
# Two kinds of component, and the difference decides re-fire behaviour:
#
#   conditions  fire while true. Pending inbox files, unread notifications,
#               unread worker events, a plans/active tree that differs from
#               the watermark. Each is a fact the beat itself is supposed to
#               clear (drain, mark read, advance, re-record), so a beat that
#               fails to clear one fires again — level-triggered, which is
#               what makes a crashed beat recoverable instead of lost.
#   deltas      fire on change against the state the last *successful* beat
#               committed (--state). Open pull requests, the child ledger,
#               Linear activity. These are surfaces where "open and waiting on
#               the operator" is a steady state that must not re-fire every
#               tick.
#
# This script never writes the state file. The wrapper commits stdout to it
# only after a beat exits cleanly, so an observation is acknowledged only by a
# reconcile that actually ran — a failed beat leaves the delta standing.
#
# Sensing is a gate, never a source of truth: the beat still reads the world
# fresh, exactly as the contract says. A miss here costs latency until the
# wrapper's resync fires, not correctness.
#
# On its own failure it fails open — the reason names the broken sensor and
# the model runs. A blind controller that stops reconciling is worse than one
# that reconciles on a false positive.
#
# Output (stdout): one JSON object,
#   {"reasons": ["..."], "components": {...}, "linear_sensed": true|false}
# reasons empty means quiet. Exit 0 either way; nonzero only for a config
# error, which the caller must treat as a reason to fire.
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FACTORY_DIR="${FACTORY_DIR:-$HOME/.factory}"

usage() { echo "Usage: $0 <instance> [--state FILE]" >&2; }
expand_home() { printf '%s\n' "${1/#\~/$HOME}"; }

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

INSTANCE=""; STATE_FILE=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --state)   [[ $# -ge 2 ]] || { usage; exit 2; }; STATE_FILE="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        -*)        usage; exit 2 ;;
        *)         INSTANCE="$1"; shift ;;
    esac
done
[[ -n "$INSTANCE" ]] || { usage; exit 2; }

CONFIG="$ROOT_DIR/factories/$INSTANCE.toml"
[[ -f "$CONFIG" ]] || { echo "factory-sense: no config: $CONFIG" >&2; exit 1; }
command -v jq &>/dev/null || { echo "factory-sense: jq not on PATH" >&2; exit 1; }

WORKDIR="$(expand_home "$(read_toml_string workspace_path "$CONFIG")")"
PLANS_BRANCH="$(read_toml_string plans_branch "$CONFIG")"
PLANS_BRANCH="${PLANS_BRANCH:-main}"
LINEAR_TEAM="$(read_toml_string linear_team "$CONFIG")"
STATE_FILE="${STATE_FILE:-$FACTORY_DIR/iterations/$INSTANCE/sense.json}"

REASONS=()
COMPONENTS="{}"

prev() { # prev <component> — what the last successful beat committed
    [[ -f "$STATE_FILE" ]] || { echo ""; return; }
    jq -r --arg k "$1" '.components[$k] // ""' "$STATE_FILE" 2>/dev/null
}

record() { # record <component> <value>
    COMPONENTS="$(jq -c --arg k "$1" --arg v "$2" '. + {($k): $v}' <<<"$COMPONENTS")"
}

reason() { REASONS+=("$1"); }

sha() { shasum -a 256 | cut -c1-16; }

# ── inbox (condition) ─────────────────────────────────────────
# Reception's relays. The beat moves handled files to done/, so anything
# sitting here is unhandled by definition.

inbox_pending=0
shopt -s nullglob
for f in "$FACTORY_DIR/inbox/$INSTANCE"/*.json; do
    inbox_pending=$(( inbox_pending + 1 ))
done
shopt -u nullglob
record inbox "$inbox_pending"
[[ "$inbox_pending" -gt 0 ]] && reason "inbox: $inbox_pending message(s) pending"

# ── plans watermark (condition) ───────────────────────────────
# The contract's own sensor, run without a model: fetch, then compare the
# plans/active tree on the plans branch against the tree at the recorded
# watermark. The beat re-records the watermark, so a standing mismatch is a
# beat that has not finished its job yet.

if [[ -d "$WORKDIR/.git" || -f "$WORKDIR/.git" ]]; then
    if git -C "$WORKDIR" fetch --quiet origin "$PLANS_BRANCH" 2>/dev/null; then
        remote_tree="$(git -C "$WORKDIR" rev-parse --verify --quiet "origin/$PLANS_BRANCH:plans/active" 2>/dev/null)"
        remote_tree="${remote_tree:-none}"
        record plans_remote "$remote_tree"
        wm_file="$WORKDIR/.factory-watermark"
        if [[ ! -f "$wm_file" ]]; then
            reason "plans: no watermark recorded (first run)"
        else
            read -r wm_branch wm_sha < "$wm_file" || true
            if [[ -z "${wm_sha:-}" ]]; then
                # A bare SHA with no branch predates the branch being recorded.
                reason "plans: watermark has no branch on it"
            elif [[ "$wm_branch" != "$PLANS_BRANCH" ]]; then
                reason "plans: watermark is on '$wm_branch', config says '$PLANS_BRANCH'"
            else
                if [[ "$wm_sha" == "genesis" ]]; then
                    wm_tree="none"
                else
                    wm_tree="$(git -C "$WORKDIR" rev-parse --verify --quiet "$wm_sha:plans/active" 2>/dev/null)"
                    wm_tree="${wm_tree:-none}"
                fi
                [[ "$remote_tree" != "$wm_tree" ]] && \
                    reason "plans: plans/active on origin/$PLANS_BRANCH differs from the watermark"
            fi
        fi
    else
        reason "sense degraded: could not fetch origin/$PLANS_BRANCH in $WORKDIR"
    fi
else
    reason "sense degraded: workspace is not a git repo: $WORKDIR"
fi

# ── worker events (condition) ─────────────────────────────────
# What the floor said since the gaffer's last read. --count never advances the
# cursor; the beat's own read at step 6 is what clears it.

events_unread="$("$ROOT_DIR/scripts/factory-events.sh" "$INSTANCE" --count --reader gaffer 2>/dev/null)"
case "$events_unread" in (''|*[!0-9]*) events_unread=0 ;; esac
record events "$events_unread"
[[ "$events_unread" -gt 0 ]] && reason "floor: $events_unread unread worker event(s)"

# ── github (condition + delta), scope-enforced ────────────────
# Unread notifications are a condition — the beat marks handled threads read,
# so unread means unanswered. Open pull requests are a delta: one awaiting the
# operator is a steady state, and what fires a beat is the set changing —
# opened, closed, merged, a new push, a comment bumping updatedAt.

SCOPE=()
while IFS= read -r line; do [[ -n "$line" ]] && SCOPE+=("$line"); done < <(awk -F= '
    $1 ~ /^[[:space:]]*repo_scope[[:space:]]*$/ {
        value=$0
        sub(/^[^=]*=[[:space:]]*\[/, "", value)
        sub(/\][[:space:]]*(#.*)?$/, "", value)
        n=split(value, parts, ",")
        for (i=1; i<=n; i++) {
            repo=parts[i]
            gsub(/^[[:space:]]*"?|"?[[:space:]]*$/, "", repo)
            if (repo != "") print repo
        }
        exit
    }' "$CONFIG")

if [[ ${#SCOPE[@]} -gt 0 ]]; then
    gh_list="$("$ROOT_DIR/scripts/factory-inbox.sh" "$INSTANCE" list 2>/dev/null)"
    gh_status=$?
    if [[ "$gh_status" -ne 0 ]]; then
        reason "sense degraded: factory-inbox list failed (exit $gh_status)"
    else
        gh_unread="$(grep -c . <<<"$gh_list" || true)"
        [[ -z "$gh_list" ]] && gh_unread=0
        record gh_unread "$gh_unread"
        [[ "$gh_unread" -gt 0 ]] && reason "github: $gh_unread unread notification(s) in scope"
    fi

    pr_dump=""
    pr_fail=0
    for repo in "${SCOPE[@]}"; do
        out="$(gh pr list --repo "$repo" --state open --json number,updatedAt 2>/dev/null)"
        if [[ $? -ne 0 ]]; then
            pr_fail=1
            reason "sense degraded: gh pr list failed for $repo"
        else
            pr_dump+="$repo $out"$'\n'
        fi
    done
    if [[ "$pr_fail" -eq 0 ]]; then
        prs_hash="$(sha <<<"$pr_dump")"
        record prs "$prs_hash"
        prev_prs="$(prev prs)"
        [[ -n "$prev_prs" && "$prs_hash" != "$prev_prs" ]] && \
            reason "github: open pull requests changed"
    fi
fi

# ── child ledger (delta) ──────────────────────────────────────
# One file per live worker, mtime bumped by the pr stamp. A stable roster of
# working workers is quiet; a worker arriving, leaving, or stamping fires.

ledger_dump=""
shopt -s nullglob
for f in "$FACTORY_DIR/children/worker-$INSTANCE-"*.json; do
    ledger_dump+="$(basename "$f") $(stat -f '%m %z' "$f" 2>/dev/null)"$'\n'
done
shopt -u nullglob
ledger_hash="$(sha <<<"$ledger_dump")"
record ledger "$ledger_hash"
prev_ledger="$(prev ledger)"
[[ -n "$prev_ledger" && "$ledger_hash" != "$prev_ledger" ]] && \
    reason "floor: child ledger changed"

# ── linear (delta, when a token is provisioned) ───────────────
# The board is where approvals and asks actually move, and the MCP session the
# beat uses is not reachable from a shell script. identity/linear is the seam:
# an executable printing a Linear API key on stdout, satisfiable by hand with
# a personal key. Without it the board is unsensed and the wrapper's resync
# owns approval latency — provisioning the token is what buys one-tick
# approvals back.

LINEAR_SENSED=false
if [[ -n "$LINEAR_TEAM" && -x "$ROOT_DIR/identity/linear" ]]; then
    token="$("$ROOT_DIR/identity/linear" 2>/dev/null)"
    if [[ -n "$token" ]]; then
        auth="$token"
        case "$token" in lin_api_*) ;; *) auth="Bearer $token" ;; esac
        gql='{"query":"query($team:String!){ issues(filter:{team:{name:{eq:$team}}}, first:50, orderBy:updatedAt){ nodes{ id updatedAt } } }","variables":{"team":"'"$LINEAR_TEAM"'"}}'
        resp="$(curl -sf --max-time 20 https://api.linear.app/graphql \
            -H "Authorization: $auth" -H "Content-Type: application/json" \
            -d "$gql" 2>/dev/null)"
        nodes="$(jq -ce '.data.issues.nodes' 2>/dev/null <<<"$resp")"
        if [[ -n "$nodes" ]]; then
            LINEAR_SENSED=true
            linear_hash="$(sha <<<"$nodes")"
            record linear "$linear_hash"
            prev_linear="$(prev linear)"
            [[ -n "$prev_linear" && "$linear_hash" != "$prev_linear" ]] && \
                reason "linear: activity on team $LINEAR_TEAM since the last beat"
        else
            reason "sense degraded: linear poll failed for team $LINEAR_TEAM"
        fi
    fi
fi

# ── report ────────────────────────────────────────────────────

reasons_json="[]"
for r in "${REASONS[@]+"${REASONS[@]}"}"; do
    reasons_json="$(jq -c --arg r "$r" '. + [$r]' <<<"$reasons_json")"
done

jq -cn --argjson reasons "$reasons_json" --argjson components "$COMPONENTS" \
      --argjson linear_sensed "$LINEAR_SENSED" \
      '{reasons: $reasons, components: $components, linear_sensed: $linear_sensed}'
