CREATE TABLE orders (
    id       INTEGER PRIMARY KEY,
    customer TEXT    NOT NULL
);

CREATE TABLE order_lines (
    id       INTEGER PRIMARY KEY,
    order_id INTEGER NOT NULL REFERENCES orders (id),
    dish     TEXT    NOT NULL,
    quantity INTEGER NOT NULL
);
