CREATE TABLE shows (
    id bigint PRIMARY KEY,
    data jsonb NOT NULL,
    tags jsonb
);

CREATE TABLE books (
    id bigint PRIMARY KEY,
    title text NOT NULL
);
