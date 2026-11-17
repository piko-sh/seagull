CREATE TABLE sales (
    id INT NOT NULL PRIMARY KEY,
    region VARCHAR(20) NOT NULL,
    city VARCHAR(20) NOT NULL,
    sold_on DATE NOT NULL,
    amount DECIMAL(10, 2) NOT NULL
);
