CREATE TABLE books (
    id        INTEGER PRIMARY KEY,
    author_id INTEGER NOT NULL REFERENCES authors (id),
    title     TEXT    NOT NULL
);
