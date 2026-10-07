CREATE TABLE stock (
    item       TEXT    NOT NULL PRIMARY KEY,
    quantity   INTEGER NOT NULL,
    deliveries INTEGER NOT NULL DEFAULT 0
);
