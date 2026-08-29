ALTER TABLE messages ADD COLUMN deleted_by INTEGER REFERENCES users(id);

-- +down
-- SQLite cannot drop columns on all supported versions; rebuild is deferred to a future
-- destructive migration path.
