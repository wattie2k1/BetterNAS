CREATE TABLE disk_notes(
    disk_id        TEXT PRIMARY KEY,
    name           TEXT NOT NULL DEFAULT '',
    notes          TEXT NOT NULL DEFAULT '',
    purchased      TEXT NOT NULL DEFAULT '',
    warranty_until TEXT NOT NULL DEFAULT ''
);
