CREATE TABLE readings (
    id        INTEGER PRIMARY KEY,
    station   TEXT    NOT NULL,
    minute    INTEGER NOT NULL,
    height_cm INTEGER NOT NULL,
    note      TEXT
);
