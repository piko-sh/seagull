CREATE SEQUENCE order_ids;

CREATE TABLE orders (
    id       BIGINT  PRIMARY KEY DEFAULT nextval('order_ids'),
    customer VARCHAR NOT NULL,
    basket   JSON    NOT NULL,
    delivery JSON
);
