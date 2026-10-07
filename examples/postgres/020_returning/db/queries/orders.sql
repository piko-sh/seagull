-- sgl.query(PlaceOrder, one)
INSERT INTO orders (item, quantity) VALUES ($1, $2)
RETURNING id, status, quantity * 250 AS price_pence;

-- sgl.query(ServeWaitingOrders, many, row: OrderLine)
UPDATE orders SET status = 'served' WHERE status = 'waiting'
RETURNING id, item, quantity;

-- sgl.query(CancelOrder, one, row: OrderLine)
DELETE FROM orders WHERE id = $1 AND status = 'waiting'
RETURNING id, item, quantity;

-- sgl.query(ReopenOrder, one, model: orders)
UPDATE orders SET status = 'waiting' WHERE id = $1
RETURNING *;
