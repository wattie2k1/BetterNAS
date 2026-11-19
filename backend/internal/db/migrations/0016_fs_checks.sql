CREATE TABLE fs_checks(
    disk_id     TEXT PRIMARY KEY,
    started_at  TEXT NOT NULL,
    finished_at TEXT NOT NULL,
    result      TEXT NOT NULL,
    online      INTEGER NOT NULL DEFAULT 0,
    tool        TEXT NOT NULL DEFAULT '',
    output      TEXT NOT NULL DEFAULT ''
);
