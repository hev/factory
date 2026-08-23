#!/bin/bash
# factory-as.sh — run a command as one of the factory's roles.
#
# Usage: scripts/factory-as.sh <role> [--] <command> [args...]
#        scripts/factory-as.sh worker -- tmux new-session -d -s worker-acme-index -c ~/workspace/acme
#
# Roles: gaffer, reception, worker.
#
# `scripts/lib/gh-auth.sh` resolves which account a role acts as, but sourcing
# it only reaches shell callers — and almost nothing here is a shell caller.
# The gaffer and every worker run `gh` themselves, from inside a claude session,
# so the only moment their identity can be decided is the moment the session is
# created. This is that moment, expressed as a wrapper: whatever it execs gets
# the role's environment and passes it down to everything it starts.
#
# The reason it exists rather than a line of `export` at each call site is the
# half nobody writes by hand. Workers are dispatched *by the gaffer*, from
# inside the gaffer's own session, so a worker session inherits the gaffer's
# GH_TOKEN unless something takes it away. A build with `identity/gaffer` and no
# `identity/worker` would then run every worker as the gaffer — silently, and
# looking exactly like it worked. So this clears the variable first and asks the
# hook second: a role with no opinion lands on ambient `gh auth`, never on
# whichever role happened to be its parent.
#
# With no `identity/` hooks at all — the shape this build ships — it is a no-op
# wrapper, and that is the point. Callers never learn which world they are in.
# See contracts/extending.md §2.
#
# ## The tmux case
#
# Exporting a variable and exec'ing is enough for a normal command, and it is
# not enough for `tmux new-session`. A new session's environment is built from
# the tmux *server's* global environment plus the handful of names in
# `update-environment` (DISPLAY, SSH_AUTH_SOCK, and friends) — the client's own
# environment is otherwise dropped at the door. So a token exported here would
# reach `tmux` and stop there, and every session would run on ambient auth while
# looking configured. Sessions are the whole point of this wrapper, so it knows
# about that one command: when the thing being run is `tmux ... new-session`, the
# token is passed as `-e GH_TOKEN=…`, which is the only way through.

set -uo pipefail

ROOT_DIR="${FACTORY_ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"

role="${1:-}"
if [[ -z "$role" ]]; then
    echo "usage: factory-as.sh <role> [--] <command> [args...]" >&2
    exit 2
fi
shift
[[ "${1:-}" == "--" ]] && shift

if [[ $# -eq 0 ]]; then
    echo "factory-as.sh: no command to run" >&2
    exit 2
fi

case "$role" in
    gaffer|reception|worker) ;;
    *)
        # Not fatal: the roles are a closed list today, and a build that grows
        # a fourth should get its command run rather than a refusal to boot.
        echo "factory-as.sh: unknown role '$role' — running with ambient auth" >&2
        ;;
esac

# Order matters. Clear, then ask.
unset GH_TOKEN

# shellcheck source=lib/gh-auth.sh
. "$ROOT_DIR/scripts/lib/gh-auth.sh"
factory_gh_auth "$role"

# `tmux new-session` needs the token handed over explicitly (see the header).
# The insert goes directly after the subcommand so it lands on new-session's own
# flags, and the scan stops there — a later literal "new-session" is an argument
# to something else, not a second subcommand.
if [[ -n "${GH_TOKEN:-}" && "$(basename -- "$1")" == "tmux" ]]; then
    argv=() inserted=0
    for arg in "$@"; do
        argv+=("$arg")
        if [[ "$inserted" -eq 0 && "$arg" == "new-session" ]]; then
            argv+=(-e "GH_TOKEN=$GH_TOKEN")
            inserted=1
        fi
    done
    if [[ "$inserted" -eq 1 ]]; then
        exec "${argv[@]}"
    fi
fi

exec "$@"
