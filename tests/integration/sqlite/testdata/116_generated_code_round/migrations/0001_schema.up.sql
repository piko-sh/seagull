CREATE TABLE shows (
    id INTEGER PRIMARY KEY,
    data JSON NOT NULL,
    tags JSON,
    payload BLOB
);

CREATE TABLE books (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL
);
