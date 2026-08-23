#!/bin/bash
# factory-context.sh — how much context this instance's gaffer is carrying.
#
# Usage: scripts/factory-context.sh <instance>
#
# Prints one integer on stdout: the gaffer's context size in tokens, right now.
# Exit 1 and a line on stderr when it cannot tell, which is not the same as
# zero — a caller that treats "cannot tell" as "plenty of room" will clear a
# session it knows nothing about.
#
# Resident runtime only. A `runtime = "one-shot"` gaffer is a fresh `claude -p`
# per beat, so its context cannot accumulate past one iteration and there is
# nothing here to measure.
#
# Where the number comes from. The harness writes every session to
# ~/.claude/projects/<slugged-cwd>/<session-id>.jsonl, and each assistant
# record carries a usage block. Context is the sum of the three input fields —
# input_tokens + cache_read_input_tokens + cache_creation_input_tokens — which
# is what the TUI is reporting when it offers "/clear to save 533.2k tokens".
# Output tokens are not context; they are already counted in the next record's
# cache read.
#
# Two traps, both hit while building this:
#
#   1. The newest .jsonl in the project directory is usually NOT the gaffer.
#      A worker in that tree, a subagent, an operator's own `claude` in the
#      workspace, and a stray `claude -p` all land in the same directory. The
#      gaffer is identified by the loop command factory-up.sh submits, which
#      appears in its transcript and in no other. Newest *among those*.
#
#   2. The last usage record is often all zeros (a cancelled or empty turn),
#      so reading the literal last record reports a context of 0 and the
#      threshold never trips. Only records that sum above zero count.

set -euo pipefail

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# The string factory-up.sh submits to start a beat. Change it there, change it
# here: this is the only thing that tells a gaffer transcript from its
# neighbours in the same directory.
LOOP_MARKER="Run the factory parent iteration"

INSTANCE="${1:-}"
[[ -n "$INSTANCE" ]] || { echo "usage: $0 <instance>" >&2; exit 2; }

CONFIG="$ROOT_DIR/factories/$INSTANCE.toml"
[[ -f "$CONFIG" ]] || { echo "factory config not found: $CONFIG" >&2; exit 1; }

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

WORKDIR="$(read_toml_string workspace_path "$CONFIG")"
WORKDIR="${WORKDIR/#\~/$HOME}"
[[ -n "$WORKDIR" ]] || { echo "missing workspace_path in $CONFIG" >&2; exit 1; }

# The harness slugs the working directory into a single directory name by
# replacing every path separator and dot with a dash, leading slash included.
SLUG="$(printf '%s\n' "$WORKDIR" | sed 's#[/.]#-#g')"
PROJECT_DIR="${CLAUDE_PROJECTS_DIR:-$HOME/.claude/projects}/$SLUG"

[[ -d "$PROJECT_DIR" ]] || {
    echo "no session directory for '$INSTANCE': $PROJECT_DIR" >&2
    exit 1
}

# Newest transcript that carries the loop command — see trap 1 above.
TRANSCRIPT=""
while IFS= read -r candidate; do
    if grep -qF "$LOOP_MARKER" "$candidate" 2>/dev/null; then
        TRANSCRIPT="$candidate"
        break
    fi
done < <(ls -t "$PROJECT_DIR"/*.jsonl 2>/dev/null || true)

[[ -n "$TRANSCRIPT" ]] || {
    echo "no gaffer transcript in $PROJECT_DIR (no session carries the loop command)" >&2
    exit 1
}

# Last usage record that sums above zero — see trap 2 above. Reading the tail
# is enough and keeps this cheap on a transcript that runs to megabytes.
CONTEXT="$(tail -400 "$TRANSCRIPT" \
    | jq -s --raw-output '
        [ .[]
          | select(.message.usage != null)
          | (.message.usage.input_tokens // 0)
            + (.message.usage.cache_read_input_tokens // 0)
            + (.message.usage.cache_creation_input_tokens // 0)
          | select(. > 0)
        ] | last // empty
    ' 2>/dev/null || true)"

[[ -n "$CONTEXT" ]] || {
    echo "no usage records in $(basename "$TRANSCRIPT")" >&2
    exit 1
}

printf '%s\n' "$CONTEXT"
