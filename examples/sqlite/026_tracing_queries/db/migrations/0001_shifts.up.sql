CREATE TABLE shifts (
    id      INTEGER PRIMARY KEY,
    keeper  TEXT    NOT NULL,
    day     TEXT    NOT NULL,
    covered INTEGER NOT NULL DEFAULT 0
);
