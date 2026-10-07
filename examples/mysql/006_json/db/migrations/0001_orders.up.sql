CREATE TABLE orders (
    id       BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    customer VARCHAR(50) NOT NULL,
    basket   JSON        NOT NULL,
    delivery JSON
);
