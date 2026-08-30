CREATE TABLE outgoing_webhooks (
  id                   TEXT PRIMARY KEY,   -- 16-byte random, base32 (same shape as webhooks.id) —
                                            -- opaque and API-exposed, must not be enumerable
  channel_id           INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  created_by           INTEGER NOT NULL REFERENCES users(id),
  name                 TEXT NOT NULL,                          -- operator-facing label
  target_url           TEXT NOT NULL,        -- validated https://, non-private host, at create and dispatch time (§4.11)
  secret               TEXT NOT NULL,        -- plaintext HMAC signing secret; never returned by any API
                                              -- response — dispatch needs the real key to sign each
                                              -- delivery, unlike an incoming webhook's token_hash which
                                              -- only ever needs to be *compared*, never recomputed from
  secret_hash          TEXT NOT NULL UNIQUE, -- sha256 of secret; kept for the SPEC.md §3.2c column shape
  secret_last4         TEXT NOT NULL,        -- last 4 chars of secret, for masked display "whsec_••••••••ab12"
  event_type           TEXT NOT NULL DEFAULT 'message.created', -- fixed value for M22; column exists for future event types
  keyword_filter       TEXT,                 -- optional case-insensitive substring; NULL/empty = fire on every message
  status               TEXT NOT NULL DEFAULT 'active', -- active | disabled | unhealthy, see §4.11
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  created_at           INTEGER NOT NULL,
  updated_at           INTEGER NOT NULL,
  last_triggered_at    INTEGER,
  last_success_at      INTEGER
);
CREATE INDEX idx_outgoing_webhooks_channel ON outgoing_webhooks(channel_id);
CREATE INDEX idx_outgoing_webhooks_creator ON outgoing_webhooks(created_by);

-- delivery log, capped per webhook at the last 50 rows (§4.11) — a debugging aid, not an
-- audit trail, so unbounded retention buys nothing and costs table growth
CREATE TABLE outgoing_webhook_deliveries (
  id                    INTEGER PRIMARY KEY,
  webhook_id            TEXT NOT NULL REFERENCES outgoing_webhooks(id) ON DELETE CASCADE,
  message_id            INTEGER,  -- deliberately NOT FK-constrained, same precedent as messages.webhook_id
                                   -- (0008_webhooks.sql) avoiding an FK cycle on migration teardown
  attempt_number        INTEGER NOT NULL, -- 1..3, see retry policy in §4.11
  request_body          TEXT NOT NULL,    -- the signed JSON payload sent, truncated to 4KB
  response_status       INTEGER,          -- NULL on timeout/connection failure
  response_body_snippet TEXT,             -- first 500 bytes of the response, for debugging a receiver
  latency_ms            INTEGER,
  created_at            INTEGER NOT NULL
);
CREATE INDEX idx_outgoing_webhook_deliveries_webhook ON outgoing_webhook_deliveries(webhook_id, id DESC);

-- +down
DROP TABLE outgoing_webhook_deliveries;
DROP TABLE outgoing_webhooks;
