CREATE TABLE smart_history(
    disk_id        TEXT    NOT NULL,
    ts             INTEGER NOT NULL,
    temp_c         INTEGER NOT NULL,
    reallocated    INTEGER NOT NULL,
    pending        INTEGER NOT NULL,
    power_on_hours INTEGER NOT NULL,
    PRIMARY KEY (disk_id, ts)
) WITHOUT ROWID;
