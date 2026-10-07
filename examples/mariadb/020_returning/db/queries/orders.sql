-- sgl.query(PlaceOrder, one)
INSERT INTO orders (item, quantity) VALUES (?, ?)
RETURNING id, status, quantity * 250 AS price_pence;

-- sgl.query(ServeWaitingOrders, execrows)
UPDATE orders SET status = 'served' WHERE status = 'waiting';

-- sgl.query(ServedOrders, many)
SELECT id, item, quantity FROM orders WHERE status = 'served' ORDER BY id;

-- sgl.query(CancelOrder, one)
DELETE FROM orders WHERE id = ? AND status = 'waiting'
RETURNING item, quantity;
