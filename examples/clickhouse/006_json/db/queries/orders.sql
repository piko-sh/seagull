-- sgl.query(PlaceOrder, exec)
INSERT INTO orders (id, customer, basket, delivery)
VALUES ({id:UInt64}, {customer:String}, {basket:String}, {delivery:Nullable(String)});

-- sgl.query(GetOrder, one)
SELECT id, customer, basket, delivery FROM orders WHERE id = {id:UInt64};

-- sgl.query(OrdersWithFish, many)
SELECT
    id,
    customer,
    JSONExtractRaw(basket, 'extras') AS extras,
    JSONExtractString(delivery, 'address') AS address
FROM orders
WHERE JSONExtractString(basket, 'fish') = {fish:String}
ORDER BY id;
