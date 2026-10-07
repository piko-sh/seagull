CREATE SEQUENCE reading_ids;

CREATE TABLE readings (
    id        BIGINT  PRIMARY KEY DEFAULT nextval('reading_ids'),
    station   VARCHAR NOT NULL,
    minute    INTEGER NOT NULL,
    height_cm INTEGER NOT NULL,
    note      VARCHAR
);
