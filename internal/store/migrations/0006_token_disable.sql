ALTER TABLE api_tokens ADD COLUMN disabled_at INTEGER;

-- +down
-- SQLite cannot drop columns on all supported versions; rebuild is deferred to a future
-- destructive migration path.
