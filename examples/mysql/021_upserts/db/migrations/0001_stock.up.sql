CREATE TABLE stock (
    item       VARCHAR(50) NOT NULL PRIMARY KEY,
    quantity   INT         NOT NULL,
    deliveries INT         NOT NULL DEFAULT 0
);
