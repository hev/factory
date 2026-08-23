#!/bin/bash
# factory-inbox.sh - Scope-enforced GitHub inbox for factory parents.
#
# The workspace defines the scope; this script enforces it. Instance loops
# are banned from the account-global `gh api notifications` — all
# notification and PR reads go through here, driven by `repo_scope` in the
# instance's factories/<instance>.toml.
#
# Usage:
#   scripts/factory-inbox.sh <instance> list
#       Unread notifications across in-scope repos only, via the per-repo
#       endpoint. TSV: repo <tab> thread_id <tab> reason <tab> subject_title
#       <tab> subject_url
#   scripts/factory-inbox.sh <instance> read <repo> <thread-id>
#       Mark one thread read. Refuses (exit 3) any repo not in repo_scope.
#   scripts/factory-inbox.sh <instance> prs
#       Open PRs you authored on in-scope repos. TSV: repo <tab> number
#       <tab> title <tab> url
#
# Auth is whatever `gh auth` already is — one account, no PAT to provision
# (scripts/lib/gh-auth.sh). A repo the account can't reach is a reported
# warning on stderr, never a silent skip.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

usage() { echo "Usage: $0 <instance> {list|read <repo> <thread-id>|prs}" >&2; }

INSTANCE="${1:-}"; COMMAND="${2:-}"
[[ -n "$INSTANCE" && -n "$COMMAND" ]] || { usage; exit 2; }

CONFIG="$ROOT_DIR/factories/$INSTANCE.toml"
[[ -f "$CONFIG" ]] || { echo "factory-inbox: no config: $CONFIG" >&2; exit 1; }

# Parse repo_scope = ["a/b", "c/d", ...] from the instance toml.
# (while-read, not mapfile: macOS /bin/bash is 3.2)
SCOPE=()
while IFS= read -r line; do SCOPE+=("$line"); done < <(awk -F= '
    $1 ~ /^[[:space:]]*repo_scope[[:space:]]*$/ {
        value=$0
        sub(/^[^=]*=[[:space:]]*\[/, "", value)
        sub(/\][[:space:]]*(#.*)?$/, "", value)
        n=split(value, parts, ",")
        for (i=1; i<=n; i++) {
            repo=parts[i]
            gsub(/^[[:space:]"]+|[[:space:]"]+$/, "", repo)
            if (repo != "") print repo
        }
        exit
    }
' "$CONFIG")
[[ ${#SCOPE[@]} -gt 0 ]] || { echo "factory-inbox: no repo_scope in $CONFIG" >&2; exit 1; }

in_scope() {
    local repo="$1" s
    for s in "${SCOPE[@]}"; do [[ "$repo" == "$s" ]] && return 0; done
    return 1
}

FACTORY_ROOT_DIR="$ROOT_DIR"
# shellcheck source=lib/gh-auth.sh
. "$ROOT_DIR/scripts/lib/gh-auth.sh"
factory_gh_auth gaffer

case "$COMMAND" in
    list)
        rc=0
        for repo in "${SCOPE[@]}"; do
            if ! gh api "repos/$repo/notifications" --paginate \
                --jq '.[] | [ .repository.full_name, .id, .reason, .subject.title, (.subject.url // "-") ] | @tsv'; then
                echo "factory-inbox: WARNING: cannot read notifications for $repo (no access?)" >&2
                rc=4
            fi
        done
        exit "$rc"
        ;;
    read)
        repo="${3:-}"; thread="${4:-}"
        [[ -n "$repo" && -n "$thread" ]] || { usage; exit 2; }
        if ! in_scope "$repo"; then
            echo "factory-inbox: REFUSED: $repo is not in $INSTANCE's repo_scope" >&2
            exit 3
        fi
        # Belt and braces: confirm the thread actually belongs to the claimed repo.
        actual=$(gh api "notifications/threads/$thread" --jq .repository.full_name)
        if [[ "$actual" != "$repo" ]]; then
            echo "factory-inbox: REFUSED: thread $thread belongs to $actual, not $repo" >&2
            exit 3
        fi
        gh api -X PATCH "notifications/threads/$thread" >/dev/null
        echo "factory-inbox: marked read: $repo thread $thread"
        ;;
    prs)
        rc=0
        for repo in "${SCOPE[@]}"; do
            if ! gh pr list --repo "$repo" --author @me --state open \
                --json number,title,url \
                --jq ".[] | [ \"$repo\", (.number|tostring), .title, .url ] | @tsv"; then
                echo "factory-inbox: WARNING: cannot list PRs for $repo (no access?)" >&2
                rc=4
            fi
        done
        exit "$rc"
        ;;
    *)
        usage; exit 2
        ;;
esac
