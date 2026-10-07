CREATE TABLE customers (
    id    INTEGER PRIMARY KEY,
    name  TEXT    NOT NULL,
    email TEXT    NOT NULL UNIQUE
);

CREATE TABLE orders (
    id          INTEGER PRIMARY KEY,
    customer_id INTEGER NOT NULL REFERENCES customers (id),
    total       INTEGER NOT NULL
);
