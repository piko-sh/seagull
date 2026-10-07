CREATE TABLE sailings (
    id          INTEGER PRIMARY KEY,
    origin      TEXT    NOT NULL,
    destination TEXT    NOT NULL,
    departs     TEXT    NOT NULL,
    seats_free  INTEGER NOT NULL
);
