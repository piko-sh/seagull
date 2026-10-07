-- sgl.query(ReceiveDelivery, one)
INSERT INTO stock (item, quantity, deliveries) VALUES ($1, $2, 1)
ON CONFLICT (item) DO UPDATE SET
    quantity   = stock.quantity + EXCLUDED.quantity,
    deliveries = stock.deliveries + 1
RETURNING item, quantity, deliveries;

-- sgl.query(AddItem, execrows)
INSERT INTO stock (item, quantity) VALUES ($1, 0)
ON CONFLICT (item) DO NOTHING;

-- sgl.query(ListStock, many)
SELECT item, quantity, deliveries FROM stock ORDER BY item;
