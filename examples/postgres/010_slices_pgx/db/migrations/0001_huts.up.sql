CREATE TABLE huts (
    number  BIGINT  PRIMARY KEY,
    beach   TEXT    NOT NULL,
    colour  TEXT    NOT NULL,
    booked  BOOLEAN NOT NULL DEFAULT false
);
