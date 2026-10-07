CREATE SEQUENCE gull_ids;

CREATE TABLE gulls (
    id       BIGINT  PRIMARY KEY DEFAULT nextval('gull_ids'),
    ring     VARCHAR NOT NULL UNIQUE,
    species  VARCHAR NOT NULL
);
