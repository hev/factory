#!/bin/bash
# notify.sh — say something outside the factory.
#
# Usage: echo "$BLOCK" | scripts/notify.sh <instance> [from]
#
# The gaffer calls this with the WAITING ON YOU block whenever the block
# changes, and again with a single line each time it dispatches a worker. A
# factory that works away at things you never hear about is a factory you have
# to go and check, which is the thing this whole rig exists to stop.
#
# `from` names the speaker and defaults to "gaffer". The front desk passes
# "reception" when it speaks first (reception-charter.md), so a reader of the
# spool can tell the loop's voice from the desk's.
#
# **Every post is also written to the event spool** — ~/.factory/events/
# <instance>.jsonl, `outward: true` — before it goes anywhere. That is how the
# front desk knows what the operator has already been told, which is the one
# thing it could never see before and the reason it used to repeat back things
# Slack carried an hour ago.
#
# ## Where the webhook lives
#
# In the login keychain, under service "hev-factory", account
# "<instance>/SLACK_WEBHOOK_URL":
#
#   security add-generic-password -U -s hev-factory \
#       -a "acme/SLACK_WEBHOOK_URL" -w "https://hooks.slack.com/services/…"
#
# `./factory init --slack-webhook …` puts it there for you. Two fallbacks are
# read if the keychain has nothing: `SLACK_WEBHOOK_URL_<INSTANCE>` in
# ~/.factory/secrets — the .env path, for a machine with no keychain — and
# `slack_webhook` in factories/<instance>.toml, which is deprecated and warns
# on every read. A credential in a config file is one that gets committed,
# screen-shared, or pasted somewhere; gitignoring the file prevents exactly one
# of those three. See scripts/lib/secrets.sh.
#
# **One channel per factory.** The webhook belongs to the instance the same way
# its repo scope does: a factory is one workspace and one job, and its channel
# is where that job reports. Two factories sharing a channel is two jobs
# interleaved in one feed.
#
# The bot-token path, for a workspace that blocks webhooks or an app you
# already run: `slack_channel = "C0123456789"` in the instance config (a
# channel id is not a secret), and SLACK_BOT_TOKEN in the keychain or
# ~/.factory/secrets, with chat:write and the bot invited. A webhook wins when
# both are present, because it is the one that needed no setup.
#
# **With neither, this exits 0 and says nothing** — after spooling. A factory
# with no Slack is a normal factory, not a broken one, its front desk still
# sees everything, and a status post is never worth failing a beat over. A post
# that is attempted and rejected is reported on stderr.
#
# This is the whole outbound surface. Somewhere else to send it — a different
# chat, a webhook, a phone — replaces this file (docs/extending.md).

set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTANCE="${1:-}"
FROM="${2:-gaffer}"
[[ -n "$INSTANCE" ]] || { echo "usage: $0 <instance> [from]" >&2; exit 2; }

CONFIG="$ROOT_DIR/factories/$INSTANCE.toml"
[[ -f "$CONFIG" ]] || { echo "notify: no config: $CONFIG" >&2; exit 1; }

text="$(cat)"
[[ -n "${text//[[:space:]]/}" ]] || { echo "notify: empty message on stdin" >&2; exit 1; }

# shellcheck source=lib/secrets.sh
. "$ROOT_DIR/scripts/lib/secrets.sh"
# shellcheck source=lib/spool.sh
. "$ROOT_DIR/scripts/lib/spool.sh"

# Spool before posting, and spool whatever happens next. The record of what the
# factory said is the front desk's, and it should not depend on Slack being up
# or configured at all.
factory_spool_append "$INSTANCE" "$FROM" posted true "$text"

config_value() {  # key
    awk -F= -v key="$1" '
        $1 ~ "^[[:space:]]*" key "[[:space:]]*$" {
            v=$0; sub(/^[^=]*=[[:space:]]*/,"",v); sub(/[[:space:]]*(#.*)?$/,"",v)
            gsub(/^"|"$/,"",v); print v; exit
        }' "$CONFIG"
}

# This factory's own webhook: keychain, then ~/.factory/secrets, then the
# deprecated config field. There is deliberately no machine-wide fallback — one
# would quietly point every factory on the machine at the same channel, which
# is the arrangement the per-factory channel exists to avoid.
webhook="$(factory_secret SLACK_WEBHOOK_URL "$INSTANCE")"
[[ -n "$webhook" ]] || webhook="$(factory_secret_from_config SLACK_WEBHOOK_URL "$CONFIG" slack_webhook)"

if [[ -n "$webhook" ]]; then
    payload="$(jq -n --arg text "$text" \
        '{text: $text, unfurl_links: false, unfurl_media: false}')"

    response="$(curl -sS -X POST -H "Content-Type: application/json; charset=utf-8" \
        --data "$payload" "$webhook")" || { echo "notify: could not reach Slack" >&2; exit 1; }

    # A webhook answers "ok" in plain text, not JSON.
    if [[ "$response" == "ok" ]]; then
        echo "notify: posted"
        exit 0
    fi
    echo "notify: Slack refused the post: $response" >&2
    exit 1
fi

channel="$(config_value slack_channel)"
token="$(factory_secret SLACK_BOT_TOKEN "$INSTANCE")"

# Nothing configured is the common case and the quiet one.
[[ -n "$channel" && -n "$token" ]] || exit 0

payload="$(jq -n --arg channel "$channel" --arg text "$text" \
    '{channel: $channel, text: $text, unfurl_links: false, unfurl_media: false}')"

response="$(curl -sS -X POST https://slack.com/api/chat.postMessage \
    -H "Authorization: Bearer $token" \
    -H "Content-Type: application/json; charset=utf-8" \
    --data "$payload")" || { echo "notify: could not reach Slack" >&2; exit 1; }

if [[ "$(jq -r '.ok' <<<"$response")" == "true" ]]; then
    echo "notify: posted to $channel"
    exit 0
fi

echo "notify: Slack refused the post: $(jq -r '.error // "unknown"' <<<"$response")" >&2
exit 1
