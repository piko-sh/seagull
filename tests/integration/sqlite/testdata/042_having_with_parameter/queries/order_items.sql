-- sgl.query(name: GetBulkCategories, command: many)
-- ?1 as sgl.param(minimum_quantity)
SELECT category, SUM(quantity) AS total_quantity
FROM order_items
GROUP BY category
HAVING SUM(quantity) >= ?1
ORDER BY total_quantity DESC;
