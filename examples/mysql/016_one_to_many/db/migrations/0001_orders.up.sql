CREATE TABLE orders (
    id       BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    customer VARCHAR(50) NOT NULL
);

CREATE TABLE order_lines (
    id       BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    order_id BIGINT      NOT NULL,
    dish     VARCHAR(50) NOT NULL,
    quantity INT         NOT NULL,
    FOREIGN KEY (order_id) REFERENCES orders (id)
);
