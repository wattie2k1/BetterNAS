CREATE TABLE snapraid_alias(
  pool_name TEXT NOT NULL REFERENCES pools(name) ON DELETE CASCADE,
  disk_id TEXT NOT NULL REFERENCES disks(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  PRIMARY KEY(pool_name, disk_id)
);
