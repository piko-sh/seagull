CREATE TABLE shows (
    id BIGINT PRIMARY KEY,
    data JSON NOT NULL,
    tags JSON
);

CREATE TABLE books (
    id BIGINT PRIMARY KEY,
    title VARCHAR NOT NULL
);
