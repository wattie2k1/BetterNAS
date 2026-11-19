CREATE TABLE disk_bench(
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    disk_id TEXT NOT NULL,
    at      TEXT NOT NULL,
    avg     REAL NOT NULL,
    min     REAL NOT NULL,
    max     REAL NOT NULL,
    samples TEXT NOT NULL
);
CREATE INDEX disk_bench_disk ON disk_bench(disk_id, id);
