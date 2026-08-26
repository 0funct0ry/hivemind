#!/usr/bin/env bash
# Seeds a fresh data directory for the Playwright e2e suite and patches the admin
# user's password to a known value.
#
# The `hivemind user passwd`/`user add` CLI prompts for a password on a real TTY
# (golang.org/x/term), so it can't be driven non-interactively from a script. Instead
# this seeds normally, then overwrites `user1`'s password_hash directly with sqlite3
# using a pre-computed bcrypt hash of E2E_PASSWORD below (cost 12, matching
# internal/auth's bcrypt cost).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DATA_DIR="${1:-/tmp/hivemind-e2e}"
BINARY="$REPO_ROOT/bin/hivemind"

# bcrypt hash (cost 12) of "e2e-test-pass-123"
E2E_PASSWORD_HASH='$2a$12$CK5363pq0ESxfO0Y6LUbj.nJ5SXSe/6warngt6mTJ/xtsK3per.LK'

rm -rf "$DATA_DIR"
"$BINARY" seed --data-dir "$DATA_DIR" --users 8 --channels 4 --messages 20000

sqlite3 "$DATA_DIR/hivemind.db" \
  "UPDATE users SET password_hash = '$E2E_PASSWORD_HASH' WHERE username = 'user1';"

echo "e2e fixture ready at $DATA_DIR (login: user1 / e2e-test-pass-123)"
