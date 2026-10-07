CREATE SEQUENCE order_ids;

CREATE TABLE orders (
    id       BIGINT  PRIMARY KEY DEFAULT nextval('order_ids'),
    item     VARCHAR NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 1,
    status   VARCHAR NOT NULL DEFAULT 'waiting'
);
