CREATE TABLE orders (
    id       INTEGER PRIMARY KEY,
    item     TEXT    NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 1,
    status   TEXT    NOT NULL DEFAULT 'waiting'
);
