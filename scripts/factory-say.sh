#!/bin/bash
# factory-say.sh — a worker's voice.
#
# Usage: scripts/factory-say.sh <instance> <from> <kind> "<one line>"
#        echo "<text>" | scripts/factory-say.sh <instance> <from> <kind> -
#
# Workers used to have no voice at all. Their state was only inferrable — a
# pane snapshot, a ledger entry, a harvest log after the fact — so the question
# "why is that one taking so long" was answered by reading pixels and guessing.
# This is the worker saying it instead: one line, in words, at the moment it is
# true.
#
# It writes to ~/.factory/events/<instance>.jsonl and nowhere else. **Nothing
# here reaches Slack.** The gaffer's channel is one job's report, and eight
# workers narrating into it is the noise a per-factory channel exists to avoid.
# The audience is the front desk (scripts/factory-events.sh), which reads the
# spool and decides what, if anything, a person needs to hear.
#
# Kinds are a closed list, because the point of the spool is that a reader can
# tell a blocker from a status line without parsing prose:
#
#   started    dispatched and working
#   blocked    stopped, needs a decision — put the decision in the text
#   pr         opened a pull request — put the URL in the text
#   done       work finished
#   failed     ended without finishing, and not on a decision
#   note       anything else worth the desk knowing; use sparingly
#
# **Say something when your state changes, and not otherwise.** The same
# discipline as the WAITING ON YOU block: a worker narrating every file it
# reads is a worker nobody will read. Five lines over a session is a talkative
# worker.
#
# Never fails the caller. A spool that cannot be written is a lost line, not a
# lost worker.

set -uo pipefail

INSTANCE="${1:-}"
FROM="${2:-}"
KIND="${3:-}"
TEXT="${4:-}"

if [[ -z "$INSTANCE" || -z "$FROM" || -z "$KIND" || -z "$TEXT" ]]; then
    echo "usage: $0 <instance> <from> <started|blocked|pr|done|failed|note> \"<text>\"" >&2
    exit 2
fi

case "$KIND" in
    started|blocked|pr|done|failed|note) ;;
    *) echo "factory-say: kind must be started, blocked, pr, done, failed or note (got: $KIND)" >&2; exit 2 ;;
esac

[[ "$TEXT" == "-" ]] && TEXT="$(cat)"
[[ -n "${TEXT//[[:space:]]/}" ]] || { echo "factory-say: empty text" >&2; exit 2; }

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/spool.sh
. "$ROOT_DIR/scripts/lib/spool.sh"

# outward=false is the whole difference between this and notify.sh, and it is
# the field the desk reads to know whether the operator has already seen this.
factory_spool_append "$INSTANCE" "$FROM" "$KIND" false "$TEXT"

echo "said: $KIND"
