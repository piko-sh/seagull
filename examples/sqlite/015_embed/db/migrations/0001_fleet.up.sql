CREATE TABLE harbours (
    id     INTEGER PRIMARY KEY,
    name   TEXT    NOT NULL,
    county TEXT    NOT NULL
);

CREATE TABLE skippers (
    id      INTEGER PRIMARY KEY,
    name    TEXT    NOT NULL,
    licence TEXT    NOT NULL
);

CREATE TABLE boats (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    harbour_id INTEGER NOT NULL REFERENCES harbours (id),
    skipper_id INTEGER REFERENCES skippers (id)
);
