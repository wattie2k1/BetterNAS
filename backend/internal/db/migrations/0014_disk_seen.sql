CREATE TABLE disk_seen(
    disk_id    TEXT PRIMARY KEY,
    first_seen TEXT NOT NULL,
    state      TEXT NOT NULL CHECK (state IN ('new', 'seen', 'ignored'))
);
