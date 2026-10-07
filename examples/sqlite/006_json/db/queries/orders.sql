-- sgl.query(PlaceOrder, exec)
INSERT INTO orders (customer, basket, delivery) VALUES (?, ?, ?);

-- sgl.query(GetOrder, one)
SELECT id, customer, basket, delivery FROM orders WHERE id = ?;

-- sgl.query(OrdersWithFish, many)
-- ?1 as sgl.param(fish, type: text)
SELECT
    id,
    customer,
    basket -> '$.extras' AS extras,
    CAST(delivery ->> '$.address' AS TEXT) AS address
FROM orders
WHERE basket ->> '$.fish' = ?1
ORDER BY id;
