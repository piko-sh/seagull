CREATE TYPE mood AS ENUM ('calm', 'stormy', 'fair');

CREATE TABLE items (
    id BIGINT PRIMARY KEY,
    kind TEXT NOT NULL,
    mood mood,
    moods mood[],
    rank INTEGER NOT NULL DEFAULT 0
);

CREATE SCHEMA IF NOT EXISTS audit_pgx;

CREATE TABLE IF NOT EXISTS audit_pgx.entries (
    id BIGINT PRIMARY KEY,
    action TEXT NOT NULL,
    actor TEXT NOT NULL
);
