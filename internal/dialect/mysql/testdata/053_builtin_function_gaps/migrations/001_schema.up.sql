CREATE TABLE readings (
    id BIGINT PRIMARY KEY,
    label VARCHAR(40) NOT NULL,
    note VARCHAR(40) NULL,
    price DECIMAL(10, 2) NOT NULL,
    ratio DOUBLE NOT NULL,
    taken_at DATETIME NOT NULL
);
