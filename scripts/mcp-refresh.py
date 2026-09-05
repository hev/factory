#!/usr/bin/env python3
"""mcp-refresh.py — keep a headless factory's MCP OAuth grants alive.

A one-shot beat runs `claude -p --strict-mcp-config`, and that path never
exercises a stored refresh token. Claude Code mints an MCP access token that
lives about 24 hours, an interactive session renews it as a side effect of
existing, and a machine whose gaffers are all headless therefore loses its
Linear tools once a day — the server is listed but exposes nothing, so the
gaffer cannot even call `authenticate`, and it files a [human step] instead.
Authorising by hand fixes it until tomorrow, which is the definition of a
recurring line that should not be one.

So: before each beat, if a grant is close to expiry, spend the refresh token.
Everything needed is already in ~/.claude/.credentials.json — client id,
issuer, refresh token — and the authorization server advertises the
refresh_token grant with `none` as an auth method, so no client secret and no
browser are involved.

Two rules this script does not break:

  - It never fails a beat. Every error path exits 0, leaving today's behaviour
    exactly as it was: the gaffer reports the [human step] and life goes on.
  - It never leaves a half-written credentials file. The file is the harness's,
    it is the only copy of the refresh token, and corrupting it would cost an
    interactive re-auth on a headless machine. Backup, temp file, atomic
    rename, mode preserved.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_CREDENTIALS = os.path.expanduser("~/.claude/.credentials.json")
# Two hours: comfortably longer than any beat interval, so a grant is renewed
# well before a beat could find it dead, and short enough that we are not
# spending a refresh token on every single beat.
DEFAULT_THRESHOLD = 2 * 60 * 60
HTTP_TIMEOUT = 15


def log(msg: str) -> None:
    print(f"mcp-refresh: {msg}", file=sys.stderr)


def token_endpoint(server_url: str) -> str:
    """Ask the authorization server where its token endpoint is.

    Falls back to the conventional path, because a discovery document that is
    briefly unreachable is not a reason to skip the refresh.
    """
    well_known = server_url.rstrip("/") + "/.well-known/oauth-authorization-server"
    try:
        with urllib.request.urlopen(well_known, timeout=HTTP_TIMEOUT) as r:
            meta = json.load(r)
        endpoint = meta.get("token_endpoint")
        if endpoint:
            return endpoint
    except Exception as exc:  # discovery is advisory, not required
        log(f"discovery failed for {server_url} ({exc}); using the default path")
    return server_url.rstrip("/") + "/token"


def refresh(entry: dict) -> dict | None:
    """Spend one refresh token. Returns the fields to merge, or None."""
    refresh_token = entry.get("refreshToken")
    client_id = entry.get("clientId")
    server = (entry.get("discoveryState") or {}).get("authorizationServerUrl") or entry.get("issuer")
    if not (refresh_token and client_id and server):
        log(f"{entry.get('serverName')}: no refresh token or client id on file — skipping")
        return None

    body = urllib.parse.urlencode(
        {
            "grant_type": "refresh_token",
            "refresh_token": refresh_token,
            "client_id": client_id,
        }
    ).encode()
    req = urllib.request.Request(
        token_endpoint(server),
        data=body,
        headers={
            "Content-Type": "application/x-www-form-urlencoded",
            "Accept": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=HTTP_TIMEOUT) as r:
            payload = json.load(r)
    except urllib.error.HTTPError as exc:
        # The body often says which of expired/revoked/wrong-client it was, and
        # that is the difference between "wait" and "re-authorise by hand".
        detail = ""
        try:
            detail = exc.read().decode()[:200]
        except Exception:
            pass
        log(f"{entry.get('serverName')}: refresh refused ({exc.code}) {detail}")
        return None
    except Exception as exc:
        log(f"{entry.get('serverName')}: refresh failed ({exc})")
        return None

    access = payload.get("access_token")
    if not access:
        log(f"{entry.get('serverName')}: token response carried no access_token")
        return None

    merged = {"accessToken": access}
    # An authorization server may rotate the refresh token. Dropping a rotated
    # one would strand the grant at the next expiry, so take it when offered.
    if payload.get("refresh_token"):
        merged["refreshToken"] = payload["refresh_token"]
    if payload.get("expires_in"):
        merged["expiresAt"] = int((time.time() + int(payload["expires_in"])) * 1000)
    return merged


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--credentials", default=DEFAULT_CREDENTIALS)
    ap.add_argument("--threshold", type=int, default=DEFAULT_THRESHOLD,
                    help="refresh a grant expiring within this many seconds")
    ap.add_argument("--server", action="append", default=[],
                    help="only this server name (repeatable); default is all")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    path = os.path.expanduser(args.credentials)
    if not os.path.exists(path):
        log(f"no credentials file at {path} — nothing to do")
        return 0
    try:
        with open(path) as fh:
            creds = json.load(fh)
    except Exception as exc:
        log(f"could not read {path} ({exc}) — leaving it alone")
        return 0

    grants = creds.get("mcpOAuth") or {}
    if not grants:
        log("no mcpOAuth grants on file — nothing to do")
        return 0

    now = time.time()
    changed = False
    for key, entry in grants.items():
        name = entry.get("serverName") or key.split("|")[0]
        if args.server and name not in args.server:
            continue
        expires_at = entry.get("expiresAt")
        if not expires_at:
            log(f"{name}: no expiry on file — skipping")
            continue
        remaining = expires_at / 1000 - now
        if remaining > args.threshold:
            log(f"{name}: {remaining / 3600:.1f}h left — no refresh needed")
            continue
        if args.dry_run:
            log(f"{name}: {remaining / 3600:.1f}h left — WOULD refresh")
            continue

        log(f"{name}: {remaining / 3600:.1f}h left — refreshing")
        merged = refresh(entry)
        if merged:
            entry.update(merged)
            changed = True
            log(f"{name}: refreshed, now valid {(entry['expiresAt'] / 1000 - time.time()) / 3600:.1f}h")

    if not changed:
        return 0

    # The file is the only copy of the refresh token. Back it up, write beside
    # it, then rename — so an interrupted write can never leave a truncated
    # credentials file where a working one used to be.
    try:
        mode = os.stat(path).st_mode & 0o777
        shutil.copy2(path, path + ".bak")
        tmp = path + ".tmp"
        with open(tmp, "w") as fh:
            json.dump(creds, fh)
        os.chmod(tmp, mode)
        os.replace(tmp, path)
        log(f"wrote {path} (previous copy at {path}.bak)")
    except Exception as exc:
        log(f"could not write {path} ({exc}) — the grant in memory was refreshed but not saved")
        return 0
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as exc:  # never fail a beat
        log(f"unexpected error ({exc}) — continuing")
        sys.exit(0)
