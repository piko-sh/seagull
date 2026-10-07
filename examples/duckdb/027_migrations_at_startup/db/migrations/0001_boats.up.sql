CREATE SEQUENCE boat_ids;

CREATE TABLE boats (
    id   BIGINT  PRIMARY KEY DEFAULT nextval('boat_ids'),
    name VARCHAR NOT NULL
);
