-- sgl.query(OrdersForCustomer, many)
-- ?1 as piko.param(customer)
SELECT id, total FROM orders WHERE customer_id = ? ORDER BY id;
