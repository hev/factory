#!/bin/bash
# reception-up.sh - Ensure a receptionist is running.
#
# Usage: reception-up.sh [instance]
#
# Every factory gets its own front desk, named the way every other session is:
#
#   factory-<instance>   this instance's receptionist
#   gaffer-<instance>    its loop
#   worker-<instance>-…  what the loop dispatched
#
# With no argument this is the bootstrap desk — session "reception", the one
# session with no instance, because a machine that has not been through
# `factory init` has no instance to name it after. Its whole job is to run that
# conversation.
#
# A receptionist is one interactive claude session in tmux, not on /loop — a
# long-running conversation booted from reception-charter.md. Idempotent: safe
# to run from launchd every few minutes. Unlike factory parents there is no
# heartbeat; liveness is simply "claude is alive in the pane".
#
# **This fire is also what wakes the desk.** A receptionist is a conversation,
# not a loop: between turns it is not executing, so it cannot notice a worker
# blocking on its own. When the event spool has lines the desk has not read,
# this sends one line into its pane telling it to go and look. The words stay
# in the spool — the same rule as gaffer-msg.sh, where content lives in the
# file and the mechanism only interrupts. Latency is one fire, 300s by default,
# against "whenever the next beat happens to close" under every other way of
# finding out.

set -euo pipefail

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTANCE="${1:-}"
CHARTER="$ROOT_DIR/reception-charter.md"
WORKDIR="$ROOT_DIR"

if [[ -n "$INSTANCE" ]]; then
    CONFIG="$ROOT_DIR/factories/$INSTANCE.toml"
    [[ -f "$CONFIG" ]] || { echo "reception-up: no config: $CONFIG" >&2; exit 1; }
    SESSION="factory-$INSTANCE"
    STATE_DIR="$HOME/.factory/reception/$INSTANCE"
else
    SESSION="reception"
    STATE_DIR="$HOME/.factory/reception"
fi

# Reception runs on Fable: it is a conversation, not a build — greeting people,
# reading the floor, drafting an RFC until it is specific enough to approve —
# and the model that talks to somebody all day should be the fast one.
# FACTORY_MODEL overrides it for the whole machine.
MODEL="${FACTORY_MODEL:-claude-fable-5}"

[[ -f "$CHARTER" ]] || { echo "charter not found: $CHARTER" >&2; exit 1; }
mkdir -p "$STATE_DIR"
touch "$STATE_DIR/transcript.md" "$STATE_DIR/notes.md" "$STATE_DIR/visitors.log"

BOOT_PROMPT="You are the receptionist. Read $CHARTER and follow it exactly: read your notes and transcript tail under $STATE_DIR, then greet whoever is here and wait."
if [[ -n "$INSTANCE" ]]; then
    BOOT_PROMPT="$BOOT_PROMPT You are the front desk for the '$INSTANCE' factory: its config is $ROOT_DIR/factories/$INSTANCE.toml, its gaffer runs in the tmux session gaffer-$INSTANCE, and its workers are worker-$INSTANCE-*. Other factories on this machine have their own desks; speak for yours. Its floor talks to you through the event spool: scripts/factory-events.sh $INSTANCE. Read it before you answer anything about the floor, and read it when you are woken."
fi

# The other time reception speaks first: a clone with no factory configured has
# nothing to be a front desk for, and setting that up is the conversation it is
# best at. The directive rides in on the boot prompt rather than being typed
# into the pane afterwards, so there is no send-keys race and no second code
# path — the skill is invoked by an agent that was told to, at the only moment
# it makes sense.
configured_factories() {
    shopt -s nullglob
    local n=0 c
    for c in "$ROOT_DIR"/factories/*.toml; do
        [[ "$(basename "$c" .toml)" == "example" ]] || n=$((n + 1))
    done
    shopt -u nullglob
    echo "$n"
}

if [[ -z "$INSTANCE" && "$(configured_factories)" -eq 0 ]]; then
    BOOT_PROMPT="$BOOT_PROMPT No factory is configured on this machine yet, so there is no fleet to report on. Run the init-factory skill now and set up the first one, before anything else."
fi

# Is claude actually running in the pane? This is the only liveness question
# worth asking, and it does not depend on what the TUI happens to draw.
receptionist_alive() {
    local pane_pid
    pane_pid="$(tmux display-message -p -t "$SESSION" '#{pane_pid}' 2>/dev/null || true)"
    [[ -n "$pane_pid" ]] && pgrep -P "$pane_pid" >/dev/null 2>&1
}

start_receptionist() {
    # Hand the boot prompt to claude as an argument instead of typing it into
    # the TUI. The old path sent "claude", waited for a '❯' to appear, typed
    # the prompt, then sent Enter up to five times while grepping the pane for
    # '⏺' to guess whether any of it had landed. Every step of that is the
    # send-keys race worth designing out: a glyph that changes
    # between releases is indistinguishable from a failure, and it stalls for a
    # minute before saying so.
    tmux send-keys -t "$SESSION" "claude --model $MODEL $(printf '%q' "$BOOT_PROMPT")" Enter

    for _ in $(seq 1 15); do
        sleep 2
        if receptionist_alive; then
            echo "reception-up: receptionist on duty in '$SESSION'"
            return 0
        fi
    done
    echo "reception-up: claude did not start in '$SESSION'" >&2
    return 1
}

# Wake the desk if the floor has said anything it has not read.
#
# Three things keep this from becoming a tap on the shoulder every five
# minutes. It carries no content, so a nudge that lands mid-thought costs one
# line and not a derailed answer. It skips an attached session, because
# somebody is already talking to the desk and injecting a line into their
# conversation is the rudeness this whole file was written to avoid. And it
# only fires when the unread count has *grown* since the last nudge, so a desk
# that is busy, or that read the spool and judged there was nothing to say, is
# asked once and then left alone until something new happens.
nudge_if_events() {
    [[ -n "$INSTANCE" ]] || return 0   # the bootstrap desk has no floor yet
    local count stamp last=0
    stamp="$STATE_DIR/last-nudge"
    count="$("$ROOT_DIR/scripts/factory-events.sh" "$INSTANCE" --count 2>/dev/null || echo 0)"
    [[ "$count" =~ ^[0-9]+$ ]] || return 0

    # Caught up: forget that we ever nudged. Without this the stamp outlives
    # the backlog it described, and the next two events would be measured
    # against an old count of two and silently swallowed.
    if [[ "$count" -eq 0 ]]; then
        rm -f "$stamp"
        return 0
    fi

    [[ "$(tmux list-clients -t "$SESSION" 2>/dev/null | wc -l | tr -d ' ')" == "0" ]] || return 0

    [[ -f "$stamp" ]] && last="$(tr -cd '0-9' < "$stamp")"
    [[ "${last:-0}" -lt "$count" ]] || return 0

    # A nudge that cannot be delivered is not worth failing the liveness run
    # over — the events keep, and the next fire tries again.
    if tmux send-keys -t "$SESSION" "EVENTS — $count unread on the floor; run scripts/factory-events.sh $INSTANCE and follow the charter" Enter 2>/dev/null; then
        printf '%s\n' "$count" > "$stamp"
        echo "reception-up: nudged '$SESSION' ($count unread)"
    fi
    return 0
}

if tmux has-session -t "$SESSION" 2>/dev/null; then
    if receptionist_alive; then
        nudge_if_events
        exit 0  # claude alive; an idle receptionist is a healthy receptionist
    fi
    echo "reception session '$SESSION' idle (claude exited); restarting"
    start_receptionist
    exit 0
fi

echo "creating reception session '$SESSION'"
# Through factory-as.sh, so the desk's `gh` calls carry reception's own account
# where a build configures one. With no identity/ hook this is the same command
# with a wrapper around it (docs/extending.md §2).
"$ROOT_DIR/scripts/factory-as.sh" reception -- \
    tmux new-session -d -s "$SESSION" -c "$WORKDIR"
start_receptionist
