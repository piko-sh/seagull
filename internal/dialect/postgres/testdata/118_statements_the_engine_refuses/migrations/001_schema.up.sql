CREATE TABLE authors (
    id bigint PRIMARY KEY,
    name text NOT NULL,
    email text,
    bio text
);

CREATE TABLE books (
    id bigint PRIMARY KEY,
    author_id bigint NOT NULL REFERENCES authors (id),
    title text NOT NULL,
    published timestamptz NOT NULL,
    tags text[] NOT NULL
);
