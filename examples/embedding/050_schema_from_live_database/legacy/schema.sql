CREATE TABLE members (
    id        INTEGER PRIMARY KEY,
    name      TEXT    NOT NULL,
    email     TEXT    UNIQUE,
    joined_on TEXT    NOT NULL DEFAULT (date('now'))
);

CREATE TABLE sightings (
    id        INTEGER PRIMARY KEY,
    member_id INTEGER NOT NULL REFERENCES members (id),
    species   TEXT    NOT NULL,
    count     INTEGER NOT NULL DEFAULT 1,
    seen_at   TEXT    NOT NULL
);
