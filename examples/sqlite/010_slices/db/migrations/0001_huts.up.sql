CREATE TABLE huts (
    number  INTEGER PRIMARY KEY,
    beach   TEXT    NOT NULL,
    colour  TEXT    NOT NULL,
    booked  BOOLEAN NOT NULL DEFAULT 0
);
