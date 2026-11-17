CREATE TYPE mood AS ENUM ('calm', 'stormy', 'fair');

CREATE TABLE items (
    id BIGINT PRIMARY KEY,
    kind VARCHAR NOT NULL,
    mood mood,
    rank INTEGER NOT NULL DEFAULT 0
);

CREATE SCHEMA audit;

CREATE TABLE audit.entries (
    id BIGINT PRIMARY KEY,
    action VARCHAR NOT NULL,
    actor VARCHAR NOT NULL
);
