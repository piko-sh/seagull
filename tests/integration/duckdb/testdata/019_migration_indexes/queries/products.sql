-- sgl.query(name: ListByCategory, command: many)
SELECT id, name, category, price, sku FROM products WHERE category = $1 ORDER BY id;

-- sgl.query(name: GetBySku, command: one)
SELECT id, name, category, price, sku FROM products WHERE sku = $1;
