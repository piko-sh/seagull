CREATE TABLE tasks (
    id         INTEGER PRIMARY KEY,
    title      TEXT    NOT NULL,
    completed  INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
