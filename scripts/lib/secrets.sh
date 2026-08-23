#!/bin/bash
# secrets.sh — where a factory's credentials actually live.
#
# Source it and call: factory_secret <NAME> [instance]
#
# Three places are consulted, in this order, and the first hit wins:
#
#   1. the login keychain  service "hev-factory", account "<instance>/<name>"
#                          (or plain "<name>" when the secret is machine-wide)
#   2. ~/.factory/secrets  NAME=value, one per line — the .env path, for a
#                          machine with no keychain or a person who wants one
#                          file they can read
#   3. factories/<instance>.toml   deprecated, warned about on every read
#
# The keychain is the answer. A credential in a config file is a credential
# that gets committed, screen-shared, or copied into a paste — factories/*.toml
# is gitignored, which prevents exactly one of those three. macOS already ships
# an encrypted store with an access-control prompt; there is no reason for this
# rig to invent a worse one.
#
# Nothing here ever fails a caller. A secret that is not set resolves to the
# empty string and the caller decides what that means — for notify.sh it means
# a factory with no Slack, which is a normal factory.

factory_secret() {  # NAME [instance]
    local name="$1" instance="${2:-}" account value

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
# config file by hand. -U updates in place rather than erroring on a duplicate.
factory_secret_set() {  # NAME value [instance]
    local name="$1" value="$2" instance="${3:-}" account="$1"
    [[ -n "$instance" ]] && account="$instance/$name"
    command -v security >/dev/null 2>&1 || {
        echo "factory_secret_set: no keychain on this machine — put ${name} in ${FACTORY_SECRETS_FILE:-$HOME/.factory/secrets}" >&2
        return 1
    }
    security add-generic-password -U -s hev-factory -a "$account" -w "$value" >/dev/null
}

# The deprecated third place, kept working so an existing machine does not
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
    echo "warn: $key is in $config — move it to the keychain:" >&2
    echo "warn:   security add-generic-password -U -s hev-factory -a '<instance>/$name' -w '<value>'" >&2
    printf '%s' "$value"
}
