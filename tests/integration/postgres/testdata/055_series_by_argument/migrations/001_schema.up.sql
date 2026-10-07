CREATE TABLE signups (
    id integer PRIMARY KEY,
    created_at timestamptz NOT NULL,
    tags text[] NOT NULL
);
