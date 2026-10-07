CREATE TABLE gulls (
    id       INTEGER PRIMARY KEY,
    ring     TEXT    NOT NULL UNIQUE,
    nickname TEXT
);

CREATE TABLE sightings (
    id      INTEGER PRIMARY KEY,
    gull_id INTEGER NOT NULL REFERENCES gulls (id),
    seen_on TEXT    NOT NULL,
    place   TEXT    NOT NULL
);
