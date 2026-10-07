CREATE TABLE authors (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE books (
    id INTEGER PRIMARY KEY,
    author_id INTEGER NOT NULL REFERENCES authors (id),
    pages INT,
    copies SMALLINT NOT NULL,
    doc TEXT NOT NULL
);
