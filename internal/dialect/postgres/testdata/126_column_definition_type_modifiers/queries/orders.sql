-- sgl.query(name: OrderLine, command: many)
SELECT x.sku, x.price, x.placed_at, x.codes
FROM orders, jsonb_to_record(orders.line) AS x (sku varchar(40), price numeric(10, 2), placed_at timestamp(3) with time zone, codes character varying(8)[])
WHERE orders.id = $1;

-- sgl.query(name: OrderLinesFromRows, command: many)
SELECT r.sku, r.price
FROM orders, ROWS FROM (jsonb_to_record(orders.line) AS (sku varchar(40), price numeric(12, 4))) AS r
WHERE orders.id = $1;
