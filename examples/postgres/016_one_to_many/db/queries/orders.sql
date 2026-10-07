-- sgl.query(OpenOrder, exec)
INSERT INTO orders (customer) VALUES ($1);

-- sgl.query(AddLine, exec)
INSERT INTO order_lines (order_id, dish, quantity) VALUES ($1, $2, $3);

-- sgl.query(ListOrders, many, group_by: orders.id)
-- sgl.embed(orders)
-- sgl.embed(order_lines)
SELECT orders.id, orders.customer, order_lines.id, order_lines.dish, order_lines.quantity
FROM orders
LEFT JOIN order_lines ON order_lines.order_id = orders.id
ORDER BY orders.id, order_lines.id;
