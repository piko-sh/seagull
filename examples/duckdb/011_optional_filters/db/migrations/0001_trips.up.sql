CREATE SEQUENCE trip_ids;

CREATE TABLE trips (
    id           BIGINT  PRIMARY KEY DEFAULT nextval('trip_ids'),
    name         VARCHAR NOT NULL,
    destination  VARCHAR NOT NULL,
    price_pence  INTEGER NOT NULL,
    dogs_welcome BOOLEAN NOT NULL
);
