CREATE TABLE sales (
    id integer PRIMARY KEY,
    region text NOT NULL,
    city text NOT NULL,
    product text NOT NULL,
    sold_on date NOT NULL,
    amount numeric NOT NULL
);
