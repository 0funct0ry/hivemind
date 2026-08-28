ALTER TABLE users ADD COLUMN avatar_file_id TEXT REFERENCES files(id);

-- +down
-- SQLite cannot drop columns on all supported versions; rebuild is deferred to a future
-- destructive migration path.
