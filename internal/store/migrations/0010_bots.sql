CREATE TABLE bots (
  user_id      INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, -- the is_bot=1 row this bot owns
  created_by   INTEGER NOT NULL REFERENCES users(id),   -- admin who created it
  description  TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'active', -- active | revoked, see §4.12
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);

-- one row per registered slash command, workspace-wide (not channel-scoped — a command is
-- available in every channel the invoking user can already post in)
CREATE TABLE slash_commands (
  id            TEXT PRIMARY KEY,   -- opaque random id, same shape as webhooks.id (§3.2b)
  trigger       TEXT NOT NULL UNIQUE COLLATE NOCASE, -- e.g. "/deploy", leading slash included, case-insensitive unique
  bot_id        INTEGER NOT NULL REFERENCES bots(user_id), -- identity a public (in_channel) response posts as
  description   TEXT NOT NULL,       -- shown in the autocomplete menu row
  syntax_hint   TEXT NOT NULL DEFAULT '', -- e.g. "<environment> <branch>", shown above the composer
  webhook_url   TEXT NOT NULL,        -- validated identically to outgoing_webhooks.target_url — no SSRF exception for commands
  secret        TEXT NOT NULL,        -- plaintext HMAC signing secret; needed to sign each synchronous execution
  secret_hash   TEXT NOT NULL UNIQUE, -- sha256 of secret; kept for the §3.2d column shape
  secret_last4  TEXT NOT NULL,        -- masked display, same convention as outgoing_webhooks
  admin_only    INTEGER NOT NULL DEFAULT 0, -- restrict execution (not visibility) to owner/admin
  status        TEXT NOT NULL DEFAULT 'active', -- active | disabled
  created_by    INTEGER NOT NULL REFERENCES users(id),
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE INDEX idx_slash_commands_status ON slash_commands(status);

-- +down
DROP TABLE slash_commands;
DROP TABLE bots;
