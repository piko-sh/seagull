CREATE SEQUENCE sailing_ids;

CREATE TABLE sailings (
    id          BIGINT  PRIMARY KEY DEFAULT nextval('sailing_ids'),
    origin      VARCHAR NOT NULL,
    destination VARCHAR NOT NULL,
    departs     VARCHAR NOT NULL,
    seats_free  INTEGER NOT NULL
);
