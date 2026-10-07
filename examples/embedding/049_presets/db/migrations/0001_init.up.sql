CREATE TABLE parcels (
    id           INTEGER PRIMARY KEY,
    recipient    TEXT    NOT NULL,
    status       TEXT    NOT NULL,
    weight_grams INTEGER NOT NULL
);
