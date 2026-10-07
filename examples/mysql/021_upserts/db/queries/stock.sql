-- sgl.query(ReceiveDelivery, execrows)
INSERT INTO stock (item, quantity, deliveries) VALUES (?, ?, 1)
ON DUPLICATE KEY UPDATE
    quantity   = quantity + VALUES(quantity),
    deliveries = deliveries + 1;

-- sgl.query(GetStock, one)
SELECT item, quantity, deliveries FROM stock WHERE item = ?;

-- sgl.query(AddItem, execrows)
INSERT INTO stock (item, quantity) VALUES (?, 0)
ON DUPLICATE KEY UPDATE item = item;

-- sgl.query(ListStock, many)
SELECT item, quantity, deliveries FROM stock ORDER BY item;
