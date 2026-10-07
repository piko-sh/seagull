CREATE TABLE stock (
    item       VARCHAR PRIMARY KEY,
    quantity   INTEGER NOT NULL,
    deliveries INTEGER NOT NULL DEFAULT 0
);
