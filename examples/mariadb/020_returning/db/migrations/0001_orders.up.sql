CREATE TABLE orders (
    id       BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    item     VARCHAR(50) NOT NULL,
    quantity INT         NOT NULL DEFAULT 1,
    status   VARCHAR(20) NOT NULL DEFAULT 'waiting'
);
