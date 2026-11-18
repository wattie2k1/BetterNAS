CREATE TABLE stack_icons(
    stack        TEXT PRIMARY KEY,
    content_type TEXT NOT NULL,
    data         BLOB NOT NULL,
    version      TEXT NOT NULL
);
