CREATE TABLE huts (
    number  BIGINT  PRIMARY KEY,
    beach   VARCHAR NOT NULL,
    colour  VARCHAR NOT NULL,
    booked  BOOLEAN NOT NULL DEFAULT false
);
