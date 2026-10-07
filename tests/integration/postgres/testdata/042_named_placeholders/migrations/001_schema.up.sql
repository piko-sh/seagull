CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    customer TEXT NOT NULL,
    title TEXT NOT NULL
);

INSERT INTO orders (id, customer, title) VALUES
    (1, 'ada', 'rope'),
    (2, 'grace', 'ada'),
    (3, 'grace', 'anchor');
