CREATE TYPE colour AS ENUM ('red', 'green', 'blue');

CREATE TABLE shows (
    id bigint PRIMARY KEY,
    data jsonb NOT NULL,
    tags jsonb,
    colours colour[] NOT NULL
);

CREATE TABLE authors (
    id bigint PRIMARY KEY,
    name text NOT NULL,
    aliases text[] NOT NULL
);

CREATE TABLE books (
    id bigint PRIMARY KEY,
    author_id bigint NOT NULL REFERENCES authors (id),
    title text NOT NULL,
    labels text[] NOT NULL
);
