CREATE TABLE tide_times (
    id      INTEGER PRIMARY KEY,
    port    TEXT    NOT NULL,
    day     INTEGER NOT NULL,
    high_at TEXT    NOT NULL,
    UNIQUE (port, day)
);
