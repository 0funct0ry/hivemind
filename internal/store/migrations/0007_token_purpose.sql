ALTER TABLE api_tokens ADD COLUMN purpose TEXT NOT NULL DEFAULT 'api_key';

-- +down
-- SQLite cannot drop columns on all supported versions; rebuild is deferred to a future
-- destructive migration path.
