-- sgl.query(name: BrowseProducts, command: many)
-- ?1 as sgl.param(category, optional: true)
-- sgl.sortable(order_by, columns: [name, price])
SELECT id, name, price, category FROM products WHERE (?1 IS NULL OR category = ?1)
