-- sgl.query(name: OrderLines, command: many)
SELECT jt.sku, jt.price, jt.placed_at
FROM orders,
     JSON_TABLE(orders.lines, '$[*]' COLUMNS (
         sku VARCHAR(40) PATH '$.sku',
         price DECIMAL(10, 2) PATH '$.price',
         placed_at DATETIME(3) PATH '$.placed_at'
     )) AS jt
WHERE orders.id = ?;
