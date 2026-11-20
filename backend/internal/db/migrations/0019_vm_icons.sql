CREATE TABLE vm_icons(
    vm           TEXT PRIMARY KEY,
    content_type TEXT NOT NULL,
    data         BLOB NOT NULL,
    version      TEXT NOT NULL
);
