CREATE TABLE notes (
    id       INTEGER PRIMARY KEY,
    title    TEXT    NOT NULL,
    body     TEXT,
    archived INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE tags (
    id      INTEGER PRIMARY KEY,
    note_id INTEGER NOT NULL REFERENCES notes (id),
    name    TEXT    NOT NULL
);
