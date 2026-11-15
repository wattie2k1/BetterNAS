CREATE TABLE disks(
  id TEXT PRIMARY KEY,
  role TEXT NOT NULL CHECK(role IN ('data','parity')),
  fs_type TEXT NOT NULL,
  fs_uuid TEXT NOT NULL,
  label TEXT NOT NULL,
  model TEXT,
  serial TEXT,
  size_bytes INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE pools(
  name TEXT PRIMARY KEY,
  create_policy TEXT NOT NULL,
  min_free TEXT NOT NULL,
  snapraid_enabled INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE TABLE pool_branches(
  pool_name TEXT NOT NULL REFERENCES pools(name) ON DELETE CASCADE,
  disk_id TEXT NOT NULL UNIQUE REFERENCES disks(id),
  position INTEGER NOT NULL,
  PRIMARY KEY(pool_name, disk_id)
);
CREATE TABLE jobs(
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  target TEXT NOT NULL,
  status TEXT NOT NULL,
  progress INTEGER NOT NULL DEFAULT 0,
  log TEXT NOT NULL DEFAULT '',
  started_by TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT
);
CREATE TABLE audit(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts TEXT NOT NULL,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL,
  result TEXT NOT NULL,
  details_json TEXT
);
CREATE TABLE sessions(
  token_hash TEXT PRIMARY KEY,
  username TEXT NOT NULL,
  csrf TEXT NOT NULL,
  expires_at TEXT NOT NULL
);
CREATE TABLE settings(
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
