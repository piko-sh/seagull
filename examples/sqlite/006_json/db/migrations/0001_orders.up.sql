CREATE TABLE orders (
    id       INTEGER PRIMARY KEY,
    customer TEXT    NOT NULL,
    basket   JSON    NOT NULL,
    delivery JSON
);
