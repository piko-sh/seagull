-- sgl.query(OrdersMentioning, many)
SELECT id, customer, title FROM orders WHERE customer = :who OR title = :who ORDER BY id;

-- sgl.query(OrdersForBuyer, many)
-- :customer as sgl.param(buyer)
SELECT id, customer, title FROM orders WHERE customer = :customer ORDER BY id;
