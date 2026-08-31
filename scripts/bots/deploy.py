#!/usr/bin/env python3
"""Sample hivemind bot: posts a deploy notification message to a channel.

Demonstrates the M23 Bot SDK contract (SPEC.md §4.12): a bot authenticates with its own
`hm_`-prefixed bearer token and posts through the existing, unmodified
`POST /channels/:id/messages` path — no bot-specific message endpoint exists. Routing bot
traffic through the same path user traffic uses is what gives it permissions, validation,
rate-limiting, idempotency, and WebSocket fanout for free.

Create the bot first (admin-only, via the Settings -> Bots UI or `POST /api/v1/bots`) and copy
its shown-once bearer token — this script never creates or manages the bot itself, it only acts
as one.

Usage:
    python3 scripts/bots/deploy.py \\
        --base-url http://localhost:8080 \\
        --token hm_XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX \\
        --channel general \\
        --environment production \\
        --branch main \\
        --status success

    # Or point at a numeric channel id, and read the token from the environment instead of the
    # command line so it never ends up in shell history / process listings:
    export HIVEMIND_BOT_TOKEN=hm_XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
    python3 scripts/bots/deploy.py --channel 3 --environment staging --branch feature/foo

Only uses the Python standard library, matching this repo's dependency-budget convention for
one-off operational scripts (see scripts/users.py).
"""

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
import uuid


def request(base_url, method, path, token, body=None):
    url = f"{base_url}{path}"
    data = json.dumps(body).encode("utf-8") if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req) as resp:
            raw = resp.read()
            return resp.status, (json.loads(raw) if raw else {})
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        try:
            payload = json.loads(raw) if raw else {}
        except json.JSONDecodeError:
            payload = {"raw": raw.decode("utf-8", "replace")}
        return exc.code, payload


def resolve_channel_id(base_url, token, channel):
    """Accepts either a numeric channel id or a #slug/name and resolves the latter to an id via
    GET /channels — the messages endpoint itself only accepts a numeric id."""
    if channel.isdigit():
        return channel

    slug = channel.lstrip("#")
    status, payload = request(base_url, "GET", "/api/v1/channels", token)
    if status != 200:
        print(f"could not list channels ({status}): {payload}", file=sys.stderr)
        sys.exit(1)

    for ch in payload.get("data", []):
        if ch.get("slug") == slug or ch.get("name") == channel:
            return ch["id"]

    print(f"no channel found matching {channel!r}", file=sys.stderr)
    sys.exit(1)


def build_body(args):
    if args.message:
        return args.message

    status_label = {
        "success": "Deploy succeeded",
        "failure": "Deploy failed",
        "started": "Deploy started",
    }.get(args.status, "Deploy update")

    lines = [f"**{status_label}**"]
    if args.environment:
        lines.append(f"Environment: `{args.environment}`")
    if args.branch:
        lines.append(f"Branch: `{args.branch}`")
    return "\n".join(lines)


def post_message(base_url, token, channel_id, body, thread_id=None):
    payload = {"body": body, "client_msg_id": str(uuid.uuid4())}
    if thread_id:
        payload["thread_id"] = thread_id

    status, resp = request(
        base_url, "POST", f"/api/v1/channels/{channel_id}/messages", token, payload
    )
    return status, resp


def main():
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument(
        "--base-url", default="http://localhost:8080", help="hivemind server base URL"
    )
    parser.add_argument(
        "--token",
        default=os.environ.get("HIVEMIND_BOT_TOKEN"),
        help="the bot's hm_-prefixed bearer token (or set HIVEMIND_BOT_TOKEN)",
    )
    parser.add_argument(
        "--user-id",
        default=os.environ.get("HIVEMIND_BOT_USER_ID"),
        help="the bot's user id, for logging only -- the token alone is what authenticates the "
        "request, exactly like a personal API key (or set HIVEMIND_BOT_USER_ID)",
    )
    parser.add_argument(
        "--channel",
        required=True,
        help="numeric channel id, or a #slug/name resolved via GET /channels",
    )
    parser.add_argument("--thread-id", help="optional numeric id of a root message to reply into")
    parser.add_argument(
        "--environment", help="e.g. production, staging -- used to build the default message body"
    )
    parser.add_argument("--branch", help="e.g. main -- used to build the default message body")
    parser.add_argument(
        "--status",
        choices=["started", "success", "failure"],
        default="success",
        help="used to build the default message body (default: success)",
    )
    parser.add_argument(
        "--message",
        help="post this exact message body instead of building one from "
        "--environment/--branch/--status",
    )
    args = parser.parse_args()

    if not args.token:
        parser.error("--token is required (or set HIVEMIND_BOT_TOKEN)")

    base_url = args.base_url.rstrip("/")

    if args.user_id:
        print(f"acting as bot user {args.user_id}")

    channel_id = resolve_channel_id(base_url, args.token, args.channel)
    body = build_body(args)

    status, resp = post_message(base_url, args.token, channel_id, body, args.thread_id)
    if status == 401:
        print("authentication failed -- the token is invalid, revoked, or regenerated", file=sys.stderr)
        sys.exit(1)
    if status not in (200, 201):
        print(f"failed to post message ({status}): {resp}", file=sys.stderr)
        sys.exit(1)

    message = resp.get("message", {})
    print(f"posted message {message.get('id')} to channel {channel_id}: {body!r}")


if __name__ == "__main__":
    main()
