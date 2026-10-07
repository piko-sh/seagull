CREATE TABLE events (
    id INTEGER PRIMARY KEY,
    created_at INTEGER NOT NULL,
    julian REAL NOT NULL,
    label TEXT NOT NULL,
    flag INTEGER NOT NULL
);
