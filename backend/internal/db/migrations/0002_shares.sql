CREATE TABLE shares(
  name TEXT PRIMARY KEY COLLATE NOCASE,
  pool TEXT NOT NULL REFERENCES pools(name),
  subdir TEXT NOT NULL,
  comment TEXT NOT NULL DEFAULT '',
  browsable INTEGER NOT NULL,
  guest INTEGER NOT NULL,
  recycle INTEGER NOT NULL,
  fruit INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE share_acl(
  share_name TEXT NOT NULL REFERENCES shares(name) ON DELETE CASCADE ON UPDATE CASCADE,
  principal_type TEXT NOT NULL CHECK(principal_type IN ('user','group')),
  principal_name TEXT NOT NULL,
  access TEXT NOT NULL CHECK(access IN ('none','ro','rw')),
  PRIMARY KEY(share_name, principal_type, principal_name)
);
