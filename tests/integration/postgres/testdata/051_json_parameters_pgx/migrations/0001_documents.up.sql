CREATE TABLE documents (
    id integer PRIMARY KEY,
    data json,
    document jsonb
);

CREATE TABLE composites (
    id integer PRIMARY KEY,
    documents jsonb[] NOT NULL,
    texts json[] NOT NULL
);
