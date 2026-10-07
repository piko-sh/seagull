-- sgl.query(name: ListByCategory, command: many)
SELECT id, name, price FROM products WHERE category = ? ORDER BY price ASC;

-- sgl.query(name: GetBySku, command: one)
SELECT id, name, category, price FROM products WHERE sku = ?;

-- sgl.query(name: ListByCategoryAndMaxPrice, command: many)
SELECT id, name, price FROM products WHERE category = ? AND price <= ? ORDER BY price ASC;
