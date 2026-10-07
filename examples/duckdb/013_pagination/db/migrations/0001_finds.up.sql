CREATE SEQUENCE find_ids;

CREATE TABLE finds (
    id       BIGINT  PRIMARY KEY DEFAULT nextval('find_ids'),
    creature VARCHAR NOT NULL,
    pool     VARCHAR NOT NULL,
    size_mm  INTEGER NOT NULL
);
