CREATE TABLE orders (
    id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    quantity INT NOT NULL,
    unit_price DECIMAL(10, 2) NOT NULL,
    weight DOUBLE NOT NULL,
    flags INT UNSIGNED NOT NULL,
    note VARCHAR(50),
    label VARCHAR(50) NOT NULL,
    separator_text VARCHAR(5),
    details JSON NOT NULL
);
