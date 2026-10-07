CREATE TABLE menu (
    id          INTEGER PRIMARY KEY,
    dish        TEXT    NOT NULL UNIQUE,
    price_pence INTEGER NOT NULL
);
