CREATE TABLE power_stats(
    disk_id     TEXT    NOT NULL,
    day         TEXT    NOT NULL,
    active_min  INTEGER NOT NULL DEFAULT 0,
    standby_min INTEGER NOT NULL DEFAULT 0,
    spinups     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (disk_id, day)
) WITHOUT ROWID;
