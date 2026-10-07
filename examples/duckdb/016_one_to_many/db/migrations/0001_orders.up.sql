CREATE SEQUENCE order_ids;

CREATE SEQUENCE order_line_ids;

CREATE TABLE orders (
    id       BIGINT  PRIMARY KEY DEFAULT nextval('order_ids'),
    customer VARCHAR NOT NULL
);

CREATE TABLE order_lines (
    id       BIGINT  PRIMARY KEY DEFAULT nextval('order_line_ids'),
    order_id BIGINT  NOT NULL REFERENCES orders (id),
    dish     VARCHAR NOT NULL,
    quantity INTEGER NOT NULL
);
