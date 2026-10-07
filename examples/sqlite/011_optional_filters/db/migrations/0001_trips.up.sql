CREATE TABLE trips (
    id           INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL,
    destination  TEXT    NOT NULL,
    price_pence  INTEGER NOT NULL,
    dogs_welcome BOOLEAN NOT NULL
);
