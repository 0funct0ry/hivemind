#!/usr/bin/env python3
"""Seed hivemind with 10 realistic users via the admin API.

Usage:
    python3 scripts/users.py --base-url http://localhost:8080 \
        --admin-user admin --admin-password <admin-password>

Only uses the Python standard library. Logs in as an existing admin, then
calls POST /api/v1/users for each seed user. All seeded users share the
password "secret123456".
"""

import argparse
import http.cookiejar
import json
import sys
import urllib.error
import urllib.request

PASSWORD = "secret123456"

# (first_name, last_name) pairs used to derive username/email/display_name.
NAMES = [
    ("Alice", "Nguyen"),
    ("Marcus", "Webb"),
    ("Priya", "Patel"),
    ("Diego", "Alvarez"),
    ("Fatima", "Khan"),
    ("Owen", "Sullivan"),
    ("Yuki", "Tanaka"),
    ("Lena", "Fischer"),
    ("Samuel", "Okafor"),
    ("Grace", "Kim"),
]


def build_users():
    users = []
    for first, last in NAMES:
        username = f"{first.lower()}.{last.lower()}"
        email = f"{first.lower()}.{last.lower()}@example.com"
        display_name = f"{first} {last}"
        users.append(
            {
                "username": username,
                "email": email,
                "display_name": display_name,
                "password": PASSWORD,
            }
        )
    return users


def make_opener():
    jar = http.cookiejar.CookieJar()
    return urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))


def request(opener, base_url, method, path, body=None):
    url = f"{base_url}{path}"
    data = json.dumps(body).encode("utf-8") if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    # Cookie-authenticated non-GET requests must carry a matching Origin
    # header to pass hivemind's CSRF check.
    req.add_header("Origin", base_url)
    try:
        with opener.open(req) as resp:
            raw = resp.read()
            return resp.status, (json.loads(raw) if raw else {})
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        try:
            payload = json.loads(raw) if raw else {}
        except json.JSONDecodeError:
            payload = {"raw": raw.decode("utf-8", "replace")}
        return exc.code, payload


def login(opener, base_url, login_name, password):
    status, payload = request(
        opener,
        base_url,
        "POST",
        "/api/v1/auth/login",
        {"login": login_name, "password": password},
    )
    if status != 200:
        print(f"login failed ({status}): {payload}", file=sys.stderr)
        sys.exit(1)
    print(f"logged in as {login_name}")


def create_user(opener, base_url, user):
    status, payload = request(opener, base_url, "POST", "/api/v1/users", user)
    if status == 201:
        print(f"created {user['username']} <{user['email']}>")
    elif status == 400 and payload.get("error", {}).get("code") == "invalid_user":
        print(f"skipped {user['username']}: {payload['error']['message']}")
    else:
        print(f"failed to create {user['username']} ({status}): {payload}", file=sys.stderr)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://localhost:8080", help="hivemind server base URL")
    parser.add_argument("--admin-user", required=True, help="existing admin username or email")
    parser.add_argument("--admin-password", required=True, help="existing admin password")
    args = parser.parse_args()

    base_url = args.base_url.rstrip("/")
    opener = make_opener()

    login(opener, base_url, args.admin_user, args.admin_password)

    for user in build_users():
        create_user(opener, base_url, user)


if __name__ == "__main__":
    main()
