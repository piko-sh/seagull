-- sgl.query(name: GetProductBySku, command: one)
SELECT id, sku, name, category, price
FROM products
WHERE sku = $1;

-- sgl.query(name: ListActiveByCategory, command: many)
SELECT id, sku, name, price
FROM products
WHERE category = $1 AND active = true
ORDER BY price;

-- sgl.query(name: FindByAttributes, command: many)
SELECT id, sku, name, attributes
FROM products
WHERE attributes @> $1::jsonb
ORDER BY id;
