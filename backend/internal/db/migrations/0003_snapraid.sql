CREATE TABLE snapraid(
  pool_name TEXT PRIMARY KEY REFERENCES pools(name) ON DELETE CASCADE,
  sync_schedule TEXT NOT NULL,
  scrub_schedule TEXT NOT NULL,
  scrub_percent INTEGER NOT NULL,
  scrub_older_days INTEGER NOT NULL,
  delete_threshold INTEGER NOT NULL
);
CREATE TABLE snapraid_parity(
  pool_name TEXT NOT NULL REFERENCES pools(name) ON DELETE CASCADE,
  disk_id TEXT NOT NULL UNIQUE REFERENCES disks(id),
  level INTEGER NOT NULL CHECK(level IN (1,2)),
  PRIMARY KEY(pool_name, level)
);
