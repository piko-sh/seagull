-- sgl.query(name: InsertProduct, command: exec)
INSERT INTO products (name, data) VALUES (?, ?);

-- sgl.query(name: GetProductPrice, command: one)
SELECT id, name, JSON_EXTRACT(data, '$.price') AS price FROM products WHERE id = ?;

-- sgl.query(name: GetProductCategory, command: one)
SELECT id, name, JSON_UNQUOTE(JSON_EXTRACT(data, '$.category')) AS category FROM products WHERE id = ?;

-- sgl.query(name: FindByCategory, command: many)
SELECT id, name, JSON_UNQUOTE(JSON_EXTRACT(data, '$.category')) AS category FROM products WHERE JSON_CONTAINS(data, JSON_OBJECT('category', ?)) ORDER BY id;

-- sgl.query(name: BuildSummary, command: one)
SELECT id, JSON_OBJECT('product_name', name, 'product_price', JSON_EXTRACT(data, '$.price')) AS summary FROM products WHERE id = ?;
