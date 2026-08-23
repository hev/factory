# gh-auth.sh — which GitHub account a factory acts as. Source, don't run.
#
#   . "$ROOT_DIR/scripts/lib/gh-auth.sh"
#   factory_gh_auth gaffer
#   gh pr list ...
#
# This build has one identity: whatever `gh auth` already is. Everything the
# factory does — dispatching, opening pull requests, marking notifications read
# — happens as the person who cloned it, with the tokens they already hold. No
# bot account, no PAT in a file, nothing to provision before the first run.
#
# `identity/<role>` is where that stops being true. If an executable of that
# name exists it is run and whatever it prints on stdout becomes GH_TOKEN for
# the calls that follow, which is how a build with separate accounts per role
# — a gaffer that is not you, a desk many people route through — plugs in
# without any of the callers below knowing about it (contracts/extending.md).
#
# Roles: gaffer, reception, worker.

factory_gh_auth() {
    local role="${1:?usage: factory_gh_auth <role>}" root hook token
    root="${FACTORY_ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
    hook="$root/identity/$role"

    [[ -x "$hook" ]] || return 0

    token="$("$hook" 2>/dev/null)" || {
        echo "gh-auth: identity/$role failed; falling back to ambient gh auth" >&2
        return 0
    }
    # An empty answer means "no opinion", which is the ambient account. Setting
    # GH_TOKEN to an empty string is not the same thing — gh treats it as an
    # attempt to authenticate with nothing and fails every call.
    [[ -n "$token" ]] && export GH_TOKEN="$token"
    return 0
}
