CREATE TABLE stock (
    item       TEXT   PRIMARY KEY,
    quantity   BIGINT NOT NULL,
    deliveries BIGINT NOT NULL DEFAULT 0
);
