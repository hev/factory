#!/bin/bash
# factory-accounts.sh — does the factory act as an account that is not the
# operator's?
#
# Usage: factory-accounts.sh <instance>
#
# One question, asked per surface, because the two surfaces are configured
# independently and a machine can easily be separated on one and shared on the
# other. Reception reads the answer to decide whether the approval acts —
# merging a pull request, setting a Linear state — are open to it at all
# (contracts/reception-charter.md, "Two accounts, or one").
#
# Output is two lines, stable and parseable:
#
#   github  reception=hev  factory=hevbot  separate=yes
#   linear  factory_server=linear-hevbot  separate=configured
#
# `separate=` is the whole answer:
#
#   yes           two logins, resolved and compared. Mechanical.
#   configured    the config names a distinct login for the factory, but this
#                 script cannot see the other side of the comparison (Linear is
#                 read through MCP, which is not a thing bash can call). The
#                 caller finishes it: reception knows which server it is using
#                 and compares the names itself.
#   no            one account. Single-player. Nothing here is separated.
#   unknown       the check could not be run — no `gh`, no network, a hook that
#                 failed. Read as `no`: an answer you could not get is not a
#                 boundary you have.
#
# **No token is ever printed.** The hook's output is resolved to a login name
# inside a subshell and the token itself never reaches stdout, a log, or a
# transcript. That matters more here than anywhere else in the build, because
# reception's whole job is writing down what it did.
#
# Exit 0 when both lines could be produced, 1 when a surface answered
# `unknown`. Nothing branches on the exit code today — read the lines — but a
# sweep that wants one gets the honest one.

set -uo pipefail
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

ROOT_DIR="${FACTORY_ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"

INSTANCE="${1:-}"
if [[ -z "$INSTANCE" ]]; then
    echo "usage: factory-accounts.sh <instance>" >&2
    exit 2
fi

CONFIG="$ROOT_DIR/factories/$INSTANCE.toml"
if [[ ! -f "$CONFIG" ]]; then
    echo "factory-accounts.sh: no config at $CONFIG" >&2
    exit 2
fi

read_toml_string() {
    awk -F= -v key="$1" '
        $1 ~ "^[[:space:]]*" key "[[:space:]]*$" {
            v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*#.*/,"",v)
            sub(/[[:space:]]*$/,"",v); gsub(/^"|"$/,"",v); print v; exit
        }' "$2"
}

# macOS ships no `timeout`, and a wedged `gh` inside a $(…) capture looks
# exactly like a command that returned nothing. Bound it the way secrets.sh
# bounds `op`: run it in the background, poll, kill.
bounded() {
    local limit="$1"; shift
    local tmp pid waited
    tmp="$(mktemp -t factory-accounts 2>/dev/null)" || return 1
    ( exec "$@" >"$tmp" 2>/dev/null ) &
    pid=$!
    waited=0
    while kill -0 "$pid" 2>/dev/null; do
        if [[ "$waited" -ge $(( limit * 10 )) ]]; then
            kill -TERM "$pid" 2>/dev/null
            wait "$pid" 2>/dev/null
            rm -f "$tmp"
            return 1
        fi
        sleep 0.1
        waited=$(( waited + 1 ))
    done
    wait "$pid" 2>/dev/null
    cat "$tmp" 2>/dev/null
    rm -f "$tmp"
    return 0
}

# The login a role's `gh` calls land as. Runs in a subshell so the role's token
# dies with it, and prints the login rather than the credential.
login_for() {
    local role="$1"
    (
        unset GH_TOKEN GITHUB_TOKEN
        # shellcheck source=lib/gh-auth.sh
        . "$ROOT_DIR/scripts/lib/gh-auth.sh"
        factory_gh_auth "$role"
        command -v gh >/dev/null 2>&1 || exit 1
        bounded 15 gh api user --jq .login
    )
}

status=0

# ---- github -----------------------------------------------------------------
# The comparison is between the two logins, not between "is a hook installed".
# A hook printing the operator's own PAT installs cleanly and separates
# nothing, and that is exactly the case a presence check would call safe.
reception_login="$(login_for reception)"
factory_login="$(login_for gaffer)"
reception_login="${reception_login//[$'\t\r\n ']/}"
factory_login="${factory_login//[$'\t\r\n ']/}"

if [[ -z "$reception_login" || -z "$factory_login" ]]; then
    gh_sep="unknown"
    status=1
elif [[ "$reception_login" == "$factory_login" ]]; then
    gh_sep="no"
else
    gh_sep="yes"
fi

printf 'github  reception=%s  factory=%s  separate=%s\n' \
    "${reception_login:-?}" "${factory_login:-?}" "$gh_sep"

# ---- linear -----------------------------------------------------------------
# `linear_mcp_server` exists for exactly one reason: the machine holds more
# than one Linear login and this factory is to use the one that is not the
# operator's (factories/example.toml). Absent means the plain `linear`
# registration, which is the operator's, which is one account.
#
# The comparison cannot be finished here. Linear is reached through MCP, so
# bash can see which server the factory is told to use and cannot see which one
# its caller is holding. Report the name and say so.
linear_server="$(read_toml_string linear_mcp_server "$CONFIG")"
linear_team="$(read_toml_string linear_team "$CONFIG")"

if [[ -z "$linear_team" ]]; then
    printf 'linear  factory_server=none  separate=n/a  (no linear_team — this factory has no board)\n'
elif [[ -z "$linear_server" ]]; then
    printf 'linear  factory_server=linear  separate=no\n'
else
    printf 'linear  factory_server=%s  separate=configured\n' "$linear_server"
fi

exit "$status"
