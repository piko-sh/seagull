CREATE TABLE products (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    price INTEGER NOT NULL
);

INSERT INTO products (id, name, price) VALUES
    (1, 'Apple', 100),
    (2, 'Banana', 50),
    (3, 'Carrot', 75),
    (4, 'Date', 200),
    (5, 'Eggplant', 150);
