CREATE TABLE lost_property (
    id          INTEGER PRIMARY KEY,
    item        TEXT    NOT NULL,
    colour      TEXT    NOT NULL,
    found_at    TEXT    NOT NULL,
    days_held   INTEGER NOT NULL,
    claimed     BOOLEAN NOT NULL DEFAULT 0,
    owner_phone TEXT
);
