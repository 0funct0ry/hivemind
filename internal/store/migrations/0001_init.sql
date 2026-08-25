PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE users (
  id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  email TEXT NOT NULL UNIQUE COLLATE NOCASE, display_name TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL, avatar_color TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT 'member', is_bot INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE sessions (
  id TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  user_agent TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE TABLE api_tokens (
  id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL, hash TEXT NOT NULL UNIQUE, last_used_at INTEGER, created_at INTEGER NOT NULL, expires_at INTEGER
);
CREATE TABLE channels (
  id INTEGER PRIMARY KEY, kind TEXT NOT NULL, slug TEXT UNIQUE COLLATE NOCASE,
  name TEXT NOT NULL DEFAULT '', topic TEXT NOT NULL DEFAULT '', dm_key TEXT UNIQUE,
  created_by INTEGER REFERENCES users(id), archived_at INTEGER, last_message_id INTEGER,
  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE INDEX idx_channels_kind ON channels(kind, archived_at);
CREATE TABLE channel_members (
  channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role TEXT NOT NULL DEFAULT 'member', joined_at INTEGER NOT NULL,
  last_read_message_id INTEGER NOT NULL DEFAULT 0, muted INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX idx_members_user ON channel_members(user_id);
CREATE TABLE messages (
  id INTEGER PRIMARY KEY, channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id), thread_id INTEGER REFERENCES messages(id) ON DELETE CASCADE,
  body TEXT NOT NULL, client_msg_id TEXT, reply_count INTEGER NOT NULL DEFAULT 0,
  last_reply_id INTEGER, has_attachments INTEGER NOT NULL DEFAULT 0, edited_at INTEGER,
  deleted_at INTEGER, created_at INTEGER NOT NULL
);
CREATE INDEX idx_msg_channel_id ON messages(channel_id, id DESC);
CREATE INDEX idx_msg_thread ON messages(thread_id, id) WHERE thread_id IS NOT NULL;
CREATE INDEX idx_msg_channel_root ON messages(channel_id, id DESC) WHERE thread_id IS NULL;
CREATE UNIQUE INDEX idx_msg_client ON messages(user_id, client_msg_id) WHERE client_msg_id IS NOT NULL;
CREATE VIRTUAL TABLE messages_fts USING fts5(body, content='messages', content_rowid='id', tokenize="unicode61 remove_diacritics 2");
CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts(rowid, body) VALUES (new.id, new.body);
END;
CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, body) VALUES('delete', old.id, old.body);
END;
CREATE TRIGGER messages_au AFTER UPDATE OF body ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, body) VALUES('delete', old.id, old.body);
  INSERT INTO messages_fts(rowid, body) VALUES (new.id, new.body);
END;
CREATE TABLE mentions (
  message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, channel_id INTEGER NOT NULL,
  kind TEXT NOT NULL, created_at INTEGER NOT NULL, PRIMARY KEY (message_id, user_id)
);
CREATE INDEX idx_mentions_user ON mentions(user_id, channel_id, message_id DESC);
CREATE TABLE files (
  id TEXT PRIMARY KEY, sha256 TEXT NOT NULL, name TEXT NOT NULL, mime TEXT NOT NULL,
  size INTEGER NOT NULL, width INTEGER, height INTEGER,
  uploaded_by INTEGER NOT NULL REFERENCES users(id), created_at INTEGER NOT NULL
);
CREATE INDEX idx_files_sha ON files(sha256);
CREATE TABLE attachments (
  message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  file_id TEXT NOT NULL REFERENCES files(id), position INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (message_id, file_id)
);
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);

-- +down
DROP TRIGGER IF EXISTS messages_ai;
DROP TRIGGER IF EXISTS messages_ad;
DROP TRIGGER IF EXISTS messages_au;
DROP TABLE IF EXISTS messages_fts;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS mentions;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS channel_members;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS schema_migrations;
