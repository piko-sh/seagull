-- sgl.query(PlaceOrder, one)
INSERT INTO orders (item, quantity) VALUES ($1, $2)
RETURNING id, status, quantity * 250 AS price_pence;

-- sgl.query(ServeWaitingOrders, many)
UPDATE orders SET status = 'served' WHERE status = 'waiting'
RETURNING id, item, quantity;

-- sgl.query(CancelOrder, one)
DELETE FROM orders WHERE id = $1 AND status = 'waiting'
RETURNING item, quantity;
