-- sgl.query(name: GetProduct, command: one)
-- ?1 as sgl.param(product_id)
SELECT id, name, sku, price, active FROM products WHERE id = ?1;
