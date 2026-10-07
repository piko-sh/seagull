CREATE SEQUENCE entry_ids;

CREATE TABLE entries (
    id             BIGINT  PRIMARY KEY DEFAULT nextval('entry_ids'),
    boat           VARCHAR NOT NULL,
    crew           INTEGER NOT NULL,
    finish_seconds INTEGER NOT NULL
);
