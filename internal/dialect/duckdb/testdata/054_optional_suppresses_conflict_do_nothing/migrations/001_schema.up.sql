CREATE SEQUENCE events_id_seq;

CREATE TABLE events (
    id BIGINT PRIMARY KEY DEFAULT nextval('events_id_seq'),
    slug VARCHAR NOT NULL UNIQUE,
    payload VARCHAR,
    archived BOOLEAN NOT NULL DEFAULT false
);
