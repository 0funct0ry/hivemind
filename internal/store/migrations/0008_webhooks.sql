CREATE TABLE webhooks (
  id            TEXT PRIMARY KEY,        -- 16-byte random, base32 (same shape as files.id) —
                                          -- opaque and URL-exposed, must not be enumerable
  channel_id    INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  bot_user_id   INTEGER NOT NULL REFERENCES users(id),   -- the is_bot=1 user this webhook posts as
  created_by    INTEGER NOT NULL REFERENCES users(id),   -- owner; see status='orphaned' below
  name          TEXT NOT NULL,                           -- operator-facing label, e.g. "Production CI"
  format_preset TEXT NOT NULL DEFAULT 'generic',          -- generic | slack_compatible
  default_display_name TEXT NOT NULL DEFAULT '',         -- card display_name when the payload has none
  default_avatar_color TEXT NOT NULL DEFAULT '',         -- card avatar color when the payload has none
  allow_payload_override INTEGER NOT NULL DEFAULT 1,     -- 0 = always use the defaults above, ignore payload's own display_name/avatar_url
  default_severity TEXT NOT NULL DEFAULT 'neutral',      -- severity assigned when a payload omits one (§4.10)
  notify_channel_on_critical INTEGER NOT NULL DEFAULT 0, -- 1 = auto-attach an @channel mention when resolved severity='critical'
  thread_id     INTEGER, -- optional: always post into this thread; not FK-constrained to
                          -- messages(id) — see note above ALTER TABLE messages below
  token_hash    TEXT NOT NULL UNIQUE,     -- sha256 of "whk_<random>"; plaintext shown once, never again
  secret_last4  TEXT NOT NULL,            -- last 4 chars of plaintext, for masked display "whk_••••••ab12"
  status        TEXT NOT NULL DEFAULT 'active', -- active | disabled | orphaned, see §4.10
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  regenerated_at INTEGER,
  last_used_at  INTEGER
);
CREATE INDEX idx_webhooks_channel ON webhooks(channel_id);
CREATE INDEX idx_webhooks_creator ON webhooks(created_by);

-- webhook-authored messages: additive columns on the existing messages table.
-- Not FK-constrained (unlike SPEC.md §3.2b's literal DDL, which declares
-- "REFERENCES webhooks(id) ON DELETE SET NULL"): messages.webhook_id and webhooks.thread_id
-- form a two-table FK cycle that the modernc.org/sqlite driver cannot unwind during a full
-- MigrateTo(0) teardown (DROP TABLE on either side leaves the other with an unresolvable
-- dangling reference, which then fails an unrelated later DROP TABLE in 0001_init.sql's frozen
-- down script). Deliberate simplification, matching mentions.channel_id's existing no-FK
-- precedent in 0001 — both ends are enforced at the application layer instead
-- (store.CreateWebhookMessage / OrphanWebhooksForUser / parseThreadIDInChannel).
ALTER TABLE messages ADD COLUMN webhook_id TEXT;
ALTER TABLE messages ADD COLUMN card TEXT;  -- JSON layout card (§4.10), NULL for ordinary messages

-- per-webhook flood-collapse bookkeeping — only the pending summary's identity needs to
-- survive a process restart; the hot-path sliding-window check itself stays in-memory
-- (same shape as internal/api/messages.go's existing postLimiter), see §4.10 and §5.5
CREATE TABLE webhook_flood_state (
  webhook_id         TEXT PRIMARY KEY REFERENCES webhooks(id) ON DELETE CASCADE,
  window_started_at  INTEGER NOT NULL,
  collapsed_count    INTEGER NOT NULL DEFAULT 0,
  summary_message_id INTEGER REFERENCES messages(id)
);

-- +down
DROP TABLE webhook_flood_state;
DROP TABLE webhooks;
