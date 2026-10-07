-- sgl.query(PlaceOrder, exec)
INSERT INTO orders (customer, basket, delivery) VALUES (?, ?, ?);

-- sgl.query(GetOrder, one)
SELECT id, customer, basket, delivery FROM orders WHERE id = ?;

-- sgl.query(OrdersWithFish, many)
-- ?1 as sgl.param(fish)
SELECT
    id,
    customer,
    JSON_EXTRACT(basket, '$.extras') AS extras,
    JSON_UNQUOTE(JSON_EXTRACT(delivery, '$.address')) AS address
FROM orders
WHERE JSON_UNQUOTE(JSON_EXTRACT(basket, '$.fish')) = ?1
ORDER BY id;
