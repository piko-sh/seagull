CREATE TABLE probes (
    id integer PRIMARY KEY,
    label text NOT NULL,
    amount bigint NOT NULL,
    ratio double precision NOT NULL,
    flag boolean NOT NULL,
    note text NOT NULL,
    payload bytea NOT NULL,
    optional_label text,
    optional_amount bigint,
    tags text[] NOT NULL,
    address inet NOT NULL
);
