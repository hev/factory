#!/bin/bash
# notify.sh — say something outside the factory.
#
# Usage: echo "$BLOCK" | scripts/notify.sh <instance> [from] [--thread <key>]
#
# The gaffer calls this with the WAITING ON YOU block whenever the block
# changes, and again with a single line each time it dispatches a worker. A
# factory that works away at things you never hear about is a factory you have
# to go and check, which is the thing this whole rig exists to stop.
#
# `from` names the speaker and defaults to "gaffer". The front desk passes
# "reception" when it speaks first (contracts/reception-charter.md), so a reader of the
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
# ## Threads
#
# `--thread <key>` names a conversation the message belongs to — the Linear
# identifier of the RFC it is about, normally. **This build ignores it**, and
# posts every message flat into the channel, because an incoming webhook
# answers `ok` in plain text: there is no `ts` in that response, so there is
# nothing for a later message to reply into. Threading is not a formatting
# choice, it is an API the webhook path does not have.
#
# The key is still parsed, still spooled, and still handed to the drop-in
# below, so a build that can thread gets it without the gaffer knowing which
# build it is talking to. A caller passes it whenever the message is about one
# RFC; the cross-RFC WAITING ON YOU block passes nothing, because a digest
# buried in one issue's thread is a digest nobody reads.
#
# ## `notify/send` — the drop-in
#
# ## Unfurling
#
# `unfurl_links: false`, `unfurl_media: true`. A block carries five links on a
# busy beat and unfurling them is the wall of text the block exists to avoid;
# an image URL still renders, so a screenshot posted as the image's own URL is
# a picture and a pull request stays one line. Post the image, not the page it
# is on.
#
# This is the whole outbound surface. Somewhere else to send it — a different
# chat, a webhook, a phone — either replaces this file
# (contracts/extending.md) or, better, is dropped in beside it:
#
#   notify/send <instance> <from> [thread-key]      message on stdin
#
# An executable at that path takes over the sending half of this script and
# nothing else. The spool line above has already been written by the time it
# runs, which is the point: a replacement cannot forget to spool, because it
# was never given the chance. Its exit status is this script's, with one
# reserved value:
#
#   66 — declined. It did not send, and this script's own path runs instead.
#
# That exit is what stops an installed drop-in from silencing a factory it
# cannot serve. A threading sender on a factory with only a webhook has no
# thread to open and no business eating the message; it declines, and the
# message goes out flat the way it did before anything was installed. Every
# other non-zero is a real refusal from the far end.
#
# `notify/` is gitignored the same way `identity/` is — see notify/README.md.

set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTANCE=""
FROM=""
THREAD=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --thread)
            THREAD="${2:-}"
            [[ -n "$THREAD" ]] || { echo "notify: --thread needs a key" >&2; exit 2; }
            shift 2
            ;;
        --thread=*) THREAD="${1#*=}"; shift ;;
        *)
            if [[ -z "$INSTANCE" ]]; then INSTANCE="$1"
            elif [[ -z "$FROM" ]]; then FROM="$1"
            else echo "notify: unexpected argument '$1'" >&2; exit 2
            fi
            shift
            ;;
    esac
done
FROM="${FROM:-gaffer}"
[[ -n "$INSTANCE" ]] || { echo "usage: $0 <instance> [from] [--thread <key>]" >&2; exit 2; }

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

# The drop-in, if a build put one here. It gets the message on stdin and the
# thread key it may or may not be able to honour, and its exit status is ours.
# Deliberately after the spool and before any credential is read: what a
# replacement sends is its business, that it was said is not.
if [[ -x "$ROOT_DIR/notify/send" ]]; then
    # Not `exec` — in a pipeline that runs in a subshell and this script would
    # carry on to post the message a second time itself.
    printf '%s' "$text" | "$ROOT_DIR/notify/send" "$INSTANCE" "$FROM" "$THREAD"
    rc=$?
    # 66 is "not mine" — fall through and send it ourselves. Anything else,
    # including success, is the drop-in's answer and ours.
    [[ $rc -ne 66 ]] && exit $rc
fi

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
        '{text: $text, unfurl_links: false, unfurl_media: true}')"

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
    '{channel: $channel, text: $text, unfurl_links: false, unfurl_media: true}')"

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
