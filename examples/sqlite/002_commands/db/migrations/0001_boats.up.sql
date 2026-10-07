CREATE TABLE boats (
    id     INTEGER PRIMARY KEY,
    name   TEXT    NOT NULL,
    berth  INTEGER NOT NULL,
    at_sea BOOLEAN NOT NULL DEFAULT 0
);
