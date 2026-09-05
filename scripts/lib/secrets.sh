#!/bin/bash
# secrets.sh — where a factory's credentials actually live.
#
# Source it and call: factory_secret <NAME> [instance]
#
# Four places are consulted, in this order, and the first hit wins:
#
#   1. 1Password        vault $FACTORY_OP_VAULT (default "layer-factory"),
#                       item "<NAME>_<INSTANCE>" then "<NAME>", field
#                       "credential" — read with `op read`
#   2. the login keychain  service "hev-factory", account "<instance>/<name>"
#                          (or plain "<name>" when the secret is machine-wide)
#   3. ~/.factory/secrets  NAME=value, one per line — the .env path, for a
#                          machine with no keychain or a person who wants one
#                          file they can read
#   4. factories/<instance>.toml   deprecated, warned about on every read
#
# 1Password is the answer, and the reason is remote provisioning. The login
# keychain refuses non-interactive access over ssh — `security` returns "User
# interaction is not allowed" — so a factory whose home_host is another machine
# could not be given its webhook without sitting at that machine. `op` with a
# service-account token has no such limit, which makes 1Password the only one of
# these four that works from anywhere. It is also already the house store: the
# same vault backs ~/shell/sync-secrets.sh, so a credential lives in one place
# instead of being rotated in two.
#
# The keychain stays second so that machines provisioned before this change keep
# working untouched. A credential in a config file is a credential that gets
# committed, screen-shared, or copied into a paste — factories/*.toml is
# gitignored, which prevents exactly one of those three.
#
# Nothing here ever fails a caller. A secret that is not set resolves to the
# empty string and the caller decides what that means — for notify.sh it means
# a factory with no Slack, which is a normal factory. That contract is why every
# lookup below swallows its own errors: an unreachable 1Password must degrade to
# the keychain, never take a beat down with it.
#
# Knobs: FACTORY_OP_VAULT (empty string disables 1Password entirely),
#        FACTORY_OP_TIMEOUT (seconds, default 10).

# Put the service-account token in the environment if a profile has not already.
#
# This is the same three lines as ~/shell/op-env.sh, and it is here because a
# beat never runs them: the launchd plist invokes `/bin/bash .../factory` —
# non-login and non-interactive — so ~/.zshenv is not sourced and nothing
# exports OP_SERVICE_ACCOUNT_TOKEN. Every lookup below then degraded to the
# keychain, which over ssh refuses non-interactively, so a headless factory
# silently resolved every secret to the empty string while the same call from a
# login shell returned it. That is why GH_TOKEN existed in the vault, verified
# by hand, and still never reached a beat.
#
# The token is the one credential that cannot come from 1Password, because it is
# what authorizes 1Password. A file readable only by its owner is the bootstrap;
# op-env.sh documents the reasoning.
factory_op_bootstrap() {
    [[ -n "${OP_SERVICE_ACCOUNT_TOKEN:-}" ]] && return 0
    local path="${OP_TOKEN_FILE:-$HOME/.config/op/token}"
    [[ -r "$path" ]] || return 0
    OP_SERVICE_ACCOUNT_TOKEN="$(cat "$path" 2>/dev/null)"
    export OP_SERVICE_ACCOUNT_TOKEN
}

# Read one op:// reference, bounded in time. `op` is normally ~2s, but a cold
# or wedged agent has hung for minutes on this machine before, and notify.sh
# runs on every beat — an unbounded call here would stall the loop rather than
# lose a Slack message. Empty output means "no value", for any reason.
factory_op_read() {  # op-reference
    local ref="$1" tmp pid waited limit value
    command -v op >/dev/null 2>&1 || return 0
    factory_op_bootstrap
    limit="${FACTORY_OP_TIMEOUT:-10}"
    tmp="$(mktemp -t factory-op 2>/dev/null)" || return 0

    # `exec` so the pid we track is op itself, not a wrapping subshell —
    # killing the subshell would leave a wedged op orphaned, once per beat.
    ( exec op read "$ref" >"$tmp" 2>/dev/null ) &
    pid=$!
    waited=0
    while kill -0 "$pid" 2>/dev/null; do
        if [[ "$waited" -ge $(( limit * 10 )) ]]; then
            kill -TERM "$pid" 2>/dev/null
            wait "$pid" 2>/dev/null
            rm -f "$tmp"
            return 0
        fi
        sleep 0.1
        waited=$(( waited + 1 ))
    done
    wait "$pid" 2>/dev/null

    # Command substitution strips the trailing newline `op read` adds, without
    # depending on --no-newline being present in this op version.
    value="$(cat "$tmp" 2>/dev/null)"
    rm -f "$tmp"
    printf '%s' "$value"
}

# "SLACK_WEBHOOK_URL" + instance "path" -> "SLACK_WEBHOOK_URL_PATH".
# The same shape ~/.factory/secrets uses, and the same shape sync-secrets.sh
# exports, so one credential has one name everywhere it can be stored.
factory_secret_title() {  # NAME [instance]
    local name="$1" instance="${2:-}"
    if [[ -n "$instance" ]]; then
        printf '%s_%s' "$name" "$(printf '%s' "$instance" | tr '[:lower:]-' '[:upper:]_')"
    else
        printf '%s' "$name"
    fi
}

factory_secret() {  # NAME [instance]
    local name="$1" instance="${2:-}" account value vault

    vault="${FACTORY_OP_VAULT-layer-factory}"
    if [[ -n "$vault" ]]; then
        # Per-instance first, then the machine-wide name — the same precedence
        # the file path below uses.
        if [[ -n "$instance" ]]; then
            value="$(factory_op_read "op://$vault/$(factory_secret_title "$name" "$instance")/credential")"
            if [[ -n "$value" ]]; then printf '%s' "$value"; return 0; fi
        fi
        value="$(factory_op_read "op://$vault/$name/credential")"
        if [[ -n "$value" ]]; then printf '%s' "$value"; return 0; fi
    fi

    if command -v security >/dev/null 2>&1; then
        account="$name"
        [[ -n "$instance" ]] && account="$instance/$name"
        value="$(security find-generic-password -s hev-factory -a "$account" -w 2>/dev/null)" || value=""
        if [[ -n "$value" ]]; then printf '%s' "$value"; return 0; fi
    fi

    local file="${FACTORY_SECRETS_FILE:-$HOME/.factory/secrets}"
    if [[ -f "$file" ]]; then
        # Per-instance first (SLACK_WEBHOOK_URL_ACME), then the bare name.
        local upper=""
        if [[ -n "$instance" ]]; then
            upper="$(printf '%s' "$instance" | tr '[:lower:]-' '[:upper:]_')"
            value="$(grep "^${name}_${upper}=" "$file" 2>/dev/null | head -1 | cut -d= -f2- | tr -d "\"'")"
            [[ -n "$value" ]] && { printf '%s' "$value"; return 0; }
        fi
        value="$(grep "^${name}=" "$file" 2>/dev/null | head -1 | cut -d= -f2- | tr -d "\"'")"
        [[ -n "$value" ]] && { printf '%s' "$value"; return 0; }
    fi

    printf ''
}

# Put one there. Used by `factory init` and by anybody moving a secret out of a
# config file by hand.
#
# Writes to 1Password when a vault is configured, because that is the copy both
# machines read; the keychain is the fallback for a machine with no `op`. -U on
# the keychain path updates in place rather than erroring on a duplicate.
factory_secret_set() {  # NAME value [instance]
    local name="$1" value="$2" instance="${3:-}" account="$1" vault title

    vault="${FACTORY_OP_VAULT-layer-factory}"
    title="$(factory_secret_title "$name" "$instance")"
    factory_op_bootstrap
    if [[ -n "$vault" ]] && command -v op >/dev/null 2>&1; then
        if op item edit "$title" --vault "$vault" "credential=$value" >/dev/null 2>&1; then
            echo "stored $title in 1Password vault $vault" >&2
            return 0
        fi
        if op item create --category "API Credential" --title "$title" \
               --vault "$vault" "credential=$value" >/dev/null 2>&1; then
            echo "created $title in 1Password vault $vault" >&2
            return 0
        fi
        echo "factory_secret_set: 1Password write failed for $title — falling back to the keychain" >&2
    fi

    [[ -n "$instance" ]] && account="$instance/$name"
    command -v security >/dev/null 2>&1 || {
        echo "factory_secret_set: no keychain and no 1Password on this machine — put ${name} in ${FACTORY_SECRETS_FILE:-$HOME/.factory/secrets}" >&2
        return 1
    }
    security add-generic-password -U -s hev-factory -a "$account" -w "$value" >/dev/null
}

# The deprecated fourth place, kept working so an existing machine does not
# break on upgrade — and kept noisy so it does not stay that way.
factory_secret_from_config() {  # NAME config-path key
    local name="$1" config="$2" key="$3" value
    [[ -f "$config" ]] || return 0
    value="$(awk -F= -v key="$key" '
        $1 ~ "^[[:space:]]*" key "[[:space:]]*$" {
            v=$0; sub(/^[^=]*=[[:space:]]*/,"",v); sub(/[[:space:]]*(#.*)?$/,"",v)
            gsub(/^"|"$/,"",v); print v; exit
        }' "$config")"
    [[ -n "$value" ]] || return 0
    echo "warn: $key is in $config — move it to 1Password:" >&2
    echo "warn:   op item create --category 'API Credential' --title '$(factory_secret_title "$name" '<instance>')' --vault '${FACTORY_OP_VAULT-layer-factory}' 'credential=<value>'" >&2
    printf '%s' "$value"
}
