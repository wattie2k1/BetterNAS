CREATE TABLE totp(
  username TEXT PRIMARY KEY,
  secret TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 0,
  last_step INTEGER NOT NULL DEFAULT 0,
  recovery_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL
);
