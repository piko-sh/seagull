-- sgl.query(PlaceOrder, exec)
INSERT INTO orders (customer, basket, delivery) VALUES ($1, $2, $3);

-- sgl.query(GetOrder, one)
-- sgl.column(basket, go_type: "seagull.sh/seagull/examples/postgres/006_json_pgx/internal/chips.Basket")
SELECT id, customer, basket, delivery FROM orders WHERE id = $1;

-- sgl.query(OrdersWithFish, many)
-- $1 as sgl.param(fish, type: text)
SELECT
    id,
    customer,
    basket -> 'extras' AS extras,
    delivery ->> 'address' AS address
FROM orders
WHERE basket ->> 'fish' = $1
ORDER BY id;

-- sgl.query(OrdersContaining, many)
-- $1 as sgl.param(subset)
SELECT id, customer
FROM orders
WHERE basket @> $1::jsonb
ORDER BY id;
