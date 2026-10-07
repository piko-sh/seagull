CREATE TABLE probes (
    id INTEGER PRIMARY KEY,
    label VARCHAR NOT NULL,
    amount BIGINT NOT NULL,
    ratio DOUBLE NOT NULL,
    flag BOOLEAN NOT NULL,
    note VARCHAR NOT NULL,
    payload BLOB NOT NULL,
    optional_label VARCHAR,
    optional_amount BIGINT,
    tags VARCHAR[] NOT NULL,
    pair STRUCT(name VARCHAR, rank BIGINT) NOT NULL,
    keyed MAP(VARCHAR, BIGINT) NOT NULL
);
