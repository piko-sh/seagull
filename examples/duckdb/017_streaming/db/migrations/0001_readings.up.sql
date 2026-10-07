CREATE SEQUENCE reading_ids;

CREATE TABLE readings (
    id       BIGINT  PRIMARY KEY DEFAULT nextval('reading_ids'),
    taken_at VARCHAR NOT NULL,
    level_cm INTEGER NOT NULL
);
