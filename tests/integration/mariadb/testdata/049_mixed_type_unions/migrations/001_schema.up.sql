CREATE TABLE readings (
    id INT PRIMARY KEY,
    delta INT NOT NULL,
    count INT UNSIGNED NOT NULL,
    total BIGINT UNSIGNED NOT NULL,
    offset_value BIGINT NOT NULL,
    label VARCHAR(10) NOT NULL,
    price DECIMAL(6, 2) NOT NULL,
    weight FLOAT NOT NULL
);
INSERT INTO readings VALUES (1, -5, 7, 18446744073709551615, -9, 'gull', 1.25, 0.5), (2, 3, 4000000000, 5, 9, 'tern', 2.10, 1.5);
